package billingmetrics

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlerExposesBillingTransactionOutcomes(t *testing.T) {
	ObserveAttempt("test_operation", "retry", "mysql_1205", 10*time.Millisecond)
	ObserveAttempt("test_operation", "success", "none", 2*time.Millisecond)
	ObserveFinal("test_operation", "success", "none", 12*time.Millisecond, 1)
	ObserveQueueWait("test_operation", time.Millisecond)
	ObserveRedisEvent("initial", "success", time.Millisecond)
	ObserveRedisMaterialized("success")
	ObserveRedisStreamState(2, 10)

	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))

	body := response.Body.String()
	if !strings.Contains(body, "new_api_billing_transaction_retries_total") {
		t.Fatalf("retry metric is missing: %s", body)
	}
	if !strings.Contains(body, "new_api_billing_transaction_final_total") {
		t.Fatalf("final outcome metric is missing: %s", body)
	}
	if !strings.Contains(body, "new_api_billing_transaction_queue_wait_seconds") {
		t.Fatalf("queue wait metric is missing: %s", body)
	}
	if !strings.Contains(body, "new_api_billing_redis_events_total") {
		t.Fatalf("Redis billing event metric is missing: %s", body)
	}
	if !strings.Contains(body, "new_api_billing_redis_materialized_total") {
		t.Fatalf("Redis billing materializer metric is missing: %s", body)
	}
	if !strings.Contains(body, "new_api_billing_redis_stream_pending") {
		t.Fatalf("Redis billing stream pending metric is missing: %s", body)
	}
}
