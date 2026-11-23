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

	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))

	body := response.Body.String()
	if !strings.Contains(body, "new_api_billing_transaction_retries_total") {
		t.Fatalf("retry metric is missing: %s", body)
	}
	if !strings.Contains(body, "new_api_billing_transaction_final_total") {
		t.Fatalf("final outcome metric is missing: %s", body)
	}
}
