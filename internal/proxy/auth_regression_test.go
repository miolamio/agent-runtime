package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInvalidCredentialsUsePreAuthBudget(t *testing.T) {
	h, token := testSetup(t)
	h.authLimiter.SetRPM(2)
	for i, credential := range []string{"unknown-one", "unknown-two", token} {
		r := httptest.NewRequest("GET", "/v1/models", nil)
		r.Header.Set("x-api-key", credential)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := http.StatusUnauthorized
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("request %d status=%d want=%d", i, w.Code, want)
		}
		if i == 2 && w.Header().Get("Retry-After") == "" {
			t.Fatal("missing retry guidance")
		}
	}
}

func TestAuthenticationConcurrencyIsBounded(t *testing.T) {
	h, token := testSetup(t)
	for i := 0; i < cap(h.authSlots); i++ {
		h.authSlots <- struct{}{}
	}
	r := httptest.NewRequest("GET", "/v1/models", nil)
	r.Header.Set("x-api-key", token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d want=429", w.Code)
	}
	<-h.authSlots
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("available slot did not admit auth: %d", w.Code)
	}
}
