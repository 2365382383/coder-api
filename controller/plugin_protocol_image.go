package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const taskPluginImageProtocolSnapshotMaxBytes = 1 << 20

func taskPluginImageBackground(request pluginruntime.ProtocolRequestContext) bool {
	body, ok := request.Body.(map[string]any)
	if !ok {
		return false
	}
	switch body["kind"] {
	case string(pluginruntime.BodyJSON):
		value, _ := body["value"].(map[string]any)
		background, _ := value["background"].(bool)
		return background
	case string(pluginruntime.BodyMultipart), string(pluginruntime.BodyForm):
		fields, _ := body["fields"].(map[string][]string)
		if values := fields["background"]; len(values) > 0 {
			return strings.EqualFold(strings.TrimSpace(values[0]), "true")
		}
	}
	return false
}

// taskPluginImageProtocolSnapshot captures the protocol request without
// gateway credentials or uploaded bytes. A large body is intentionally left
// out; the task can still be retrieved and the renderer receives its task
// view and stable protocol identity.
func taskPluginImageProtocolSnapshot(request pluginruntime.ProtocolRequestContext) *model.TaskProtocolRequestSnapshot {
	snapshot := &model.TaskProtocolRequestSnapshot{
		Path:          request.Path,
		Method:        request.Method,
		Params:        cloneTaskProtocolParams(request.Params),
		Query:         cloneTaskProtocolQuery(request.Query),
		Protocol:      request.Protocol,
		Operation:     request.Operation,
		Model:         request.Model,
		UpstreamModel: request.UpstreamModel,
		Stream:        request.Stream,
		Background:    taskPluginImageBackground(request),
	}
	if body, err := common.Marshal(request.Body); err == nil && len(body) <= taskPluginImageProtocolSnapshotMaxBytes {
		snapshot.Body = body
	}
	return snapshot
}

func cloneTaskProtocolParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(params))
	for key, value := range params {
		cloned[key] = value
	}
	return cloned
}

func cloneTaskProtocolQuery(query map[string][]string) map[string][]string {
	if len(query) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(query))
	for key, values := range query {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func taskPluginImageProtocolRequestFromTask(c *gin.Context, task *model.Task) (pluginruntime.ProtocolRequestContext, bool) {
	snapshot := task.PrivateData.OpenAIImageProtocol
	if snapshot == nil || snapshot.Protocol != pluginruntime.ProtocolOpenAIImage {
		return pluginruntime.ProtocolRequestContext{}, false
	}
	body := any(map[string]any{"kind": string(pluginruntime.BodyNone)})
	if len(snapshot.Body) > 0 {
		var decoded any
		if err := common.Unmarshal(snapshot.Body, &decoded); err == nil && decoded != nil {
			body = decoded
		}
	}
	path := snapshot.Path
	if path == "" {
		path = c.Request.URL.Path
	}
	method := snapshot.Method
	if method == "" {
		method = http.MethodPost
	}
	modelName := snapshot.Model
	if modelName == "" {
		modelName = task.Properties.OriginModelName
	}
	return pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{
			Path:   path,
			Method: method,
			Params: cloneTaskProtocolParams(snapshot.Params),
			Query:  cloneTaskProtocolQuery(snapshot.Query),
			Body:   body,
		},
		Protocol:      snapshot.Protocol,
		Operation:     snapshot.Operation,
		Model:         modelName,
		UpstreamModel: snapshot.UpstreamModel,
		Stream:        snapshot.Stream,
	}, true
}

// serveTaskPluginImageProtocol answers an OpenAI Images API request that a
// task plugin claimed. Synchronous requests retain the historical in-request
// polling behavior. A request with background:true returns the durable task
// immediately and uses the image retrieve route for the later render.
func serveTaskPluginImageProtocol(c *gin.Context, pinned pluginruntime.PinnedEndpoint, deps pluginProtocolBridgeDeps) {
	deps = deps.withDefaults()
	requestValue, exists := c.Get(pluginruntime.ContextKeyProtocolRequest)
	protocolRequest, ok := requestValue.(pluginruntime.ProtocolRequestContext)
	if !exists || !ok || protocolRequest.Protocol != pinned.Protocol || pinned.Plugin == nil {
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return
	}
	clientRequest := c.Request
	var relayInfo *relaycommon.RelayInfo
	var relayInfoErr error
	var outcome *taskSubmissionOutcome
	var taskErr *dto.TaskError
	// The request plugins' report hooks receive the submission once the
	// client has the response.
	defer func() {
		requestplugin.Report(c, types.RelayFormatTask, relayInfo, nil, taskReportError(taskErr))
	}()
	// DashScope bills the images as soon as it accepts the upstream call. A
	// client that gives up while the submission is in flight must not cancel
	// that call, the task row, its settlement, or its consume log, so the
	// submission runs on its own bounded context exactly like the Responses
	// bridge. Only the in-request wait and the response write observe the
	// client connection.
	func() {
		submissionContext, cancelSubmission := context.WithTimeout(
			context.WithoutCancel(clientRequest.Context()),
			deps.submissionTimeout,
		)
		c.Request = clientRequest.Clone(submissionContext)
		defer func() {
			c.Request = clientRequest
			cancelSubmission()
		}()

		relayInfo, relayInfoErr = relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
		if relayInfoErr != nil {
			return
		}
		relayInfo.IsStream = false
		relayInfo.OriginModelName = c.GetString("resolved_task_model")
		if action := c.GetString("task_action"); action != "" {
			relayInfo.Action = action
		}
		if taskErr = relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
			return
		}
		if taskErr = relay.ApplyOriginTaskAffinity(c, relayInfo); taskErr != nil {
			return
		}
		outcome, taskErr = deps.submit(c, relayInfo)
	}()
	if clientRequest.Context().Err() != nil {
		// The client is gone. A durable task keeps its settlement and consume
		// log; an asynchronous one is finished by the background poller.
		return
	}
	if relayInfoErr != nil {
		logger.LogError(c, common.LogText("build task protocol relay info failed: %s", relayInfoErr.Error()))
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return
	}
	if taskErr != nil {
		respondTaskPluginImageError(c, taskErr)
		return
	}
	if outcome == nil || outcome.Task == nil || outcome.Task.Platform != constant.TaskPlatform(pinned.Plugin.Meta.Key) {
		logger.LogError(c, common.LogText("task protocol submission returned an invalid durable outcome"))
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return
	}
	task := outcome.Task
	background := taskPluginImageBackground(protocolRequest)
	if !background && task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure {
		if taskErr = waitTaskPluginImageTask(c, task, deps); taskErr != nil {
			if c.Request.Context().Err() == nil {
				respondTaskPluginImageError(c, taskErr)
			}
			return
		}
	}
	if task.Status == model.TaskStatusFailure {
		if background {
			respondTaskPluginImageBackground(c, task, protocolRequest, http.StatusOK)
			return
		}
		reason := strings.TrimSpace(task.FailReason)
		if reason == "" {
			reason = "image generation failed"
		}
		// The fail reason comes from the upstream, so mask the channel address in it.
		taskErr = service.TaskErrorWrapper(errors.New(reason), "image_generation_failed", http.StatusBadRequest, common.UpstreamAddresses(c)...)
		taskErr.LocalError = true
		respondTaskPluginImageError(c, taskErr)
		return
	}
	if background && task.Status != model.TaskStatusSuccess {
		respondTaskPluginImageBackground(c, task, protocolRequest, http.StatusAccepted)
		return
	}
	response, ok := renderTaskPluginImageResponse(c, pinned, protocolRequest, task, deps)
	if !ok {
		return
	}
	if background {
		addTaskPluginImageEnvelope(response, task, protocolRequest)
	}
	c.JSON(http.StatusOK, response)
}

func renderTaskPluginImageResponse(c *gin.Context, pinned pluginruntime.PinnedEndpoint, protocolRequest pluginruntime.ProtocolRequestContext, task *model.Task, deps pluginProtocolBridgeDeps) (map[string]any, bool) {
	view, err := service.BuildTaskPluginView(task)
	if err != nil {
		logger.LogError(c, common.LogText("build task protocol view failed: %s", err.Error()))
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return nil, false
	}
	viewValue, err := taskPluginProtocolJSONValue(view)
	if err != nil {
		logger.LogError(c, common.LogText("encode task protocol view failed: %s", err.Error()))
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return nil, false
	}
	payload, err := pinned.Plugin.Engine.CallPathWithAdmissionTimeout(c.Request.Context(), deps.admissionTimeout, "protocols", []string{pinned.Protocol, "render"}, protocolRequest.JSValue(), viewValue)
	if err != nil {
		logger.LogError(c, common.LogText("task protocol image render hook failed: %s", err.Error()))
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return nil, false
	}
	response, ok := payload.(map[string]any)
	if !ok {
		logger.LogError(c, common.LogText("task protocol image render hook must return an object with a data array"))
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return nil, false
	}
	data, hasData := response["data"].([]any)
	if !hasData {
		logger.LogError(c, common.LogText("task protocol image render hook must return an object with a data array"))
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return nil, false
	}
	if _, present := response["created"]; !present {
		response["created"] = taskPluginImageCreatedAt(task)
	}
	// response_format is host-owned: the plugin renders upstream URLs and the
	// host inlines each image when the client asked for Base64.
	if taskPluginImageResponseFormat(protocolRequest) == "b64_json" {
		for _, entry := range data {
			item, isObject := entry.(map[string]any)
			if !isObject {
				continue
			}
			url, _ := item["url"].(string)
			if existing, _ := item["b64_json"].(string); strings.TrimSpace(url) == "" || existing != "" {
				continue
			}
			_, encoded, downloadErr := deps.downloadImage(url)
			if downloadErr != nil {
				logger.LogWarn(c, common.LogText("task protocol image download failed; returning the upstream URL instead of b64_json: %s", common.MaskSensitiveInfo(downloadErr.Error(), common.UpstreamAddresses(c)...)))
				continue
			}
			item["b64_json"] = encoded
		}
	}
	return response, true
}

func taskPluginImageCreatedAt(task *model.Task) int64 {
	createdAt := task.CreatedAt
	if createdAt == 0 {
		createdAt = task.SubmitTime
	}
	return createdAt
}

func taskPluginImageStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "completed"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusInProgress:
		return "in_progress"
	default:
		return "queued"
	}
}

func addTaskPluginImageEnvelope(response map[string]any, task *model.Task, request pluginruntime.ProtocolRequestContext) {
	response["id"] = task.TaskID
	response["object"] = "image_generation"
	response["status"] = taskPluginImageStatus(task.Status)
	response["model"] = request.Model
	response["created"] = taskPluginImageCreatedAt(task)
}

func taskPluginImageEnvelope(task *model.Task, request pluginruntime.ProtocolRequestContext) map[string]any {
	envelope := map[string]any{
		"id":      task.TaskID,
		"object":  "image_generation",
		"status":  taskPluginImageStatus(task.Status),
		"model":   request.Model,
		"created": taskPluginImageCreatedAt(task),
	}
	if task.Status == model.TaskStatusFailure {
		reason := strings.TrimSpace(task.FailReason)
		if reason == "" {
			reason = "image generation failed"
		}
		envelope["error"] = map[string]any{"message": reason, "code": "image_generation_failed"}
	}
	return envelope
}

func respondTaskPluginImageBackground(c *gin.Context, task *model.Task, request pluginruntime.ProtocolRequestContext, status int) {
	response := taskPluginImageEnvelope(task, request)
	if failure, ok := response["error"].(map[string]any); ok {
		if message, ok := failure["message"].(string); ok {
			failure["message"] = common.MaskSensitiveInfo(message, common.UpstreamAddresses(c)...)
		}
	}
	c.JSON(status, response)
}

// RetrieveTaskPluginImage returns a durable asynchronous Images API task. The
// task is loaded through the authenticated owner scope, then the plugin that
// created it renders the terminal task using the persisted request context.
func RetrieveTaskPluginImage(c *gin.Context) {
	retrieveTaskPluginImage(c, defaultPluginProtocolBridgeDeps())
}

func retrieveTaskPluginImage(c *gin.Context, deps pluginProtocolBridgeDeps) {
	deps = deps.withDefaults()
	taskID := strings.TrimSpace(c.Param("task_id"))
	if !strings.HasPrefix(taskID, "task_") {
		writeTaskPluginImageNotFound(c, taskID, "bad_prefix")
		return
	}
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	task, exists, err := deps.getByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c, common.LogText("task protocol image retrieve lookup failed"))
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return
	}
	if !exists || task == nil || !task.ResultRetrievable() || task.PrivateData.OpenAIImageProtocol == nil {
		writeTaskPluginImageNotFound(c, taskID, "missing")
		return
	}
	plugin, generation, ok := deps.resolvePlugin(task.Platform)
	if !ok || plugin == nil {
		writeTaskPluginImageNotFound(c, taskID, "no_plugin")
		return
	}
	claimsProtocol := false
	for _, claim := range plugin.Meta.Protocols {
		if claim.Name == pluginruntime.ProtocolOpenAIImage {
			claimsProtocol = true
			break
		}
	}
	if !claimsProtocol {
		writeTaskPluginImageNotFound(c, taskID, "no_claim")
		return
	}
	protocolRequest, ok := taskPluginImageProtocolRequestFromTask(c, task)
	if !ok {
		writeTaskPluginImageNotFound(c, taskID, "invalid_snapshot")
		return
	}
	if protocolRequest.Operation == "" {
		protocolRequest.Operation = "generate"
	}
	pinned := pluginruntime.PinnedEndpoint{
		Generation: generation,
		Plugin:     plugin,
		Protocol:   pluginruntime.ProtocolOpenAIImage,
		Operation:  pluginruntime.HostProtocolOperation{Name: "retrieve", Methods: []string{http.MethodGet}, Path: c.Request.URL.Path, BodyKinds: []pluginruntime.BodyKind{pluginruntime.BodyNone}, RequiredProtocolMembers: []string{"render"}},
		Model:      protocolRequest.Model,
	}
	if task.Status != model.TaskStatusSuccess {
		respondTaskPluginImageBackground(c, task, protocolRequest, http.StatusOK)
		return
	}
	response, rendered := renderTaskPluginImageResponse(c, pinned, protocolRequest, task, deps)
	if !rendered {
		return
	}
	addTaskPluginImageEnvelope(response, task, protocolRequest)
	c.JSON(http.StatusOK, response)
}

func writeTaskPluginImageNotFound(c *gin.Context, taskID, reason string) {
	logger.LogDebug(c, "task_plugin subsystem=protocol event=image_retrieve_not_found reason=%s task_id=%q", reason, taskID)
	respondPluginProtocolError(c, http.StatusNotFound, "not_found", "No image task found with id '"+taskID+"'.")
}

// waitTaskPluginImageTask polls an asynchronous image task inside the client
// request through the ordinary task polling path, so completion settles
// billing exactly as the background poller would, until the task is terminal
// or the protocol timeout elapses. A client disconnect stops waiting; the
// background poller then finishes the durable task.
func waitTaskPluginImageTask(c *gin.Context, task *model.Task, deps pluginProtocolBridgeDeps) *dto.TaskError {
	ctx, cancel := context.WithTimeout(c.Request.Context(), deps.submissionTimeout)
	defer cancel()
	for {
		timer := time.NewTimer(deps.imagePollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && c.Request.Context().Err() == nil {
				return service.TaskErrorWrapperLocal(fmt.Errorf("image generation is still running; task %s settles when it completes", task.TaskID), "task_timeout", http.StatusGatewayTimeout)
			}
			return service.TaskErrorWrapperLocal(ctx.Err(), "request_cancelled", http.StatusRequestTimeout)
		case <-timer.C:
		}
		if err := deps.pollTask(ctx, task); err != nil {
			logger.LogWarn(c, common.LogText("task protocol image poll failed for task %s: %s", task.TaskID, common.MaskSensitiveInfo(err.Error(), common.UpstreamAddresses(c)...)))
			continue
		}
		if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
			return nil
		}
	}
}

// pollTaskPluginImageTask runs one poll round for a single task through the
// same code path as the scheduled task poller, including status CAS and
// completion settlement.
func pollTaskPluginImageTask(ctx context.Context, task *model.Task) error {
	upstreamID := task.GetUpstreamTaskID()
	if upstreamID == "" {
		return errors.New("task has no upstream id")
	}
	return service.UpdateVideoTasks(ctx, task.Platform, map[int][]string{task.ChannelId: {upstreamID}}, map[string]*model.Task{upstreamID: task})
}

func taskPluginImageResponseFormat(request pluginruntime.ProtocolRequestContext) string {
	body, ok := request.Body.(map[string]any)
	if !ok {
		return ""
	}
	switch body["kind"] {
	case string(pluginruntime.BodyJSON):
		value, _ := body["value"].(map[string]any)
		format, _ := value["response_format"].(string)
		return strings.TrimSpace(format)
	case string(pluginruntime.BodyMultipart), string(pluginruntime.BodyForm):
		fields, _ := body["fields"].(map[string][]string)
		if values := fields["response_format"]; len(values) > 0 {
			return strings.TrimSpace(values[0])
		}
	}
	return ""
}

// respondTaskPluginImageError writes an OpenAI error envelope for the Images
// API. DashScope failure bodies arrive as JSON {code, message}; their message
// is surfaced so a client sees the vendor reason instead of raw JSON.
func respondTaskPluginImageError(c *gin.Context, taskErr *dto.TaskError) {
	service.RecordRequestPolicyTermination(c, taskSubmissionAPIError(taskErr))
	status := http.StatusInternalServerError
	if taskErr.StatusCode >= 400 && taskErr.StatusCode <= 599 {
		status = taskErr.StatusCode
	}
	code := taskErr.Code
	message := taskErr.Message
	var upstream struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if common.UnmarshalJsonStr(message, &upstream) == nil && strings.TrimSpace(upstream.Message) != "" {
		message = upstream.Message
		if strings.TrimSpace(upstream.Code) != "" {
			code = upstream.Code
		}
	}
	errorType := "new_api_error"
	if status < http.StatusInternalServerError {
		errorType = "invalid_request_error"
	}
	c.JSON(status, gin.H{"error": gin.H{
		"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
		"type":    errorType,
		"code":    code,
	}})
}
