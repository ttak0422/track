package webui

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func TestBoundInvalidSignalExcludesRequestCancellationAndLocalFailures(t *testing.T) {
	for _, tc := range []struct {
		err     error
		invalid bool
	}{
		{fmt.Errorf("sticky drift: %w", config.ErrLaunchManifestChanged), true},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
		{fmt.Errorf("local peer unavailable"), false},
	} {
		response := httptest.NewRecorder()
		writeBoundInvalid(response, tc.err)
		if response.Code != 503 || (response.Header().Get("X-Track-Bound-Invalid") == "1") != tc.invalid {
			t.Fatalf("wrong invalidation signal for %v: %d %v", tc.err, response.Code, response.Header())
		}
	}
}
