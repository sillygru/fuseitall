// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetPairStatusFresh(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	st := svc.GetPairStatus()
	if st.Listening {
		t.Fatal("fresh service must not be listening yet")
	}
	if st.LastAcceptUnix != 0 || st.LastRejectUnix != 0 || st.LastRejectKind != "" {
		t.Fatalf("fresh status = %+v, want zero stamps", st)
	}
}

func TestGetPairStatusQRCoordinates(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	svc.ConfigurePairing("Mac", "macos", "192.168.1.2", 18789, "fp", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "192.168.1.2,192.168.1.3")
	svc.markListening(":18789")
	st := svc.GetPairStatus()
	if !st.Listening || st.QRHost != "192.168.1.2" || st.QRPort != 18789 {
		t.Fatalf("status = %+v, want listening QR coordinates", st)
	}
	if len(st.QRCandidates) != 2 || st.QRCandidates[0] != "192.168.1.2" {
		t.Fatalf("candidates = %q, want 2 hosts", st.QRCandidates)
	}
}

func TestWrapHandlerRecordsAcceptAndReject(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	body := `{"protocol_v":1,"type":"ping","sender":{"platform":"android","app_build":1,"min_peer_build":1},"capabilities":["ping"],"payload":{"nonce":"n","reply_port":18790}}`
	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(body))
	req.RemoteAddr = "192.168.1.5:50000"
	rec := httptest.NewRecorder()
	WrapHandler(svc, ok).ServeHTTP(rec, req)
	if got := svc.GetPairStatus(); got.LastAcceptUnix == 0 {
		t.Fatal("200 ping must stamp LastAcceptUnix")
	}

	forbidden := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	req2 := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(body))
	rec2 := httptest.NewRecorder()
	WrapHandler(svc, forbidden).ServeHTTP(rec2, req2)
	if got := svc.GetPairStatus(); got.LastRejectKind != "auth" || got.LastRejectUnix == 0 {
		t.Fatalf("403 must stamp auth reject: %+v", got)
	}

	upgrade := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUpgradeRequired)
		_, _ = w.Write([]byte(`{"protocol_v":1,"type":"error","sender":{"platform":"macos","app_build":13,"min_peer_build":1},"capabilities":["ping"],"payload":{"code":"UPDATE_REQUIRED","message":"m","required_build":13}}`))
	})
	req3 := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(body))
	rec3 := httptest.NewRecorder()
	WrapHandler(svc, upgrade).ServeHTTP(rec3, req3)
	if got := svc.GetPairStatus(); got.LastRejectKind != "update" {
		t.Fatalf("426 must stamp update reject: %+v", got)
	}
}

func TestSplitCandidates(t *testing.T) {
	got := splitCandidates("a, a ,,b")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("= %q, want [a b]", got)
	}
	if len(splitCandidates("")) != 0 {
		t.Fatal("empty must be empty")
	}
}
