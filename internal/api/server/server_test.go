package server

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pkgserver "github.com/DjordjeVuckovic/tusker/pkg/server"
	"github.com/labstack/echo/v4"
)

func startOnLoopback(t *testing.T, s *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.Echo.Listener = ln
	s.Echo.HideBanner = true
	s.Echo.HidePort = true
	go func() { _ = s.Echo.Start("") }()
	t.Cleanup(func() { _ = s.Echo.Close() })
	return ln.Addr().String()
}

func TestServer_DropsConnectionThatNeverFinishesHeaders(t *testing.T) {
	s := New(&Config{Port: "0", ReadHeaderTimeout: 50 * time.Millisecond}, pkgserver.NewOkHealthChecker()).
		SetupHealthChecks("/health")
	addr := startOnLoopback(t, s)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := io.WriteString(conn, "GET /health HTTP/1.1\r\nHost: localhost\r\n"); err != nil {
		t.Fatalf("write partial header: %v", err)
	}

	// The deadline only stops a hanging server from hanging the test.
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	_, err = conn.Read(make([]byte, 1))

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatal("server kept a connection with a partial header open")
	}
	if err == nil {
		t.Fatal("server answered a request whose headers never finished")
	}
}

func TestServer_RejectsBodyOverLimit(t *testing.T) {
	tests := []struct {
		name     string
		bodySize int
		wantCode int
	}{
		{name: "under the limit", bodySize: 512, wantCode: http.StatusOK},
		{name: "over the limit", bodySize: 4096, wantCode: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(&Config{Port: "0", BodyLimit: "1K"}, pkgserver.NewOkHealthChecker()).
				SetupMiddlewares().
				SetupErrorHandler()
			s.Echo.POST("/echo", func(c echo.Context) error {
				body, err := io.ReadAll(c.Request().Body)
				if err != nil {
					return err
				}
				return c.String(http.StatusOK, string(body))
			})

			req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(strings.Repeat("a", tt.bodySize)))
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestLoadConfig_ServerBounds(t *testing.T) {
	t.Run("env overrides", func(t *testing.T) {
		t.Setenv("HTTP_READ_HEADER_TIMEOUT", "2s")
		t.Setenv("HTTP_WRITE_TIMEOUT", "3m")
		t.Setenv("HTTP_BODY_LIMIT", "64K")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.ReadHeaderTimeout != 2*time.Second {
			t.Errorf("ReadHeaderTimeout = %v, want 2s", cfg.ReadHeaderTimeout)
		}
		if cfg.WriteTimeout != 3*time.Minute {
			t.Errorf("WriteTimeout = %v, want 3m", cfg.WriteTimeout)
		}
		if cfg.BodyLimit != "64K" {
			t.Errorf("BodyLimit = %q, want 64K", cfg.BodyLimit)
		}
	})

	t.Run("unset leaves no timeout unbounded", func(t *testing.T) {
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		for name, d := range map[string]time.Duration{
			"ReadHeaderTimeout": cfg.ReadHeaderTimeout,
			"ReadTimeout":       cfg.ReadTimeout,
			"WriteTimeout":      cfg.WriteTimeout,
			"IdleTimeout":       cfg.IdleTimeout,
		} {
			if d <= 0 {
				t.Errorf("%s = %v, want a positive default", name, d)
			}
		}
		if cfg.BodyLimit == "" {
			t.Error("BodyLimit is empty, want a default")
		}
	})

	invalid := []struct {
		name  string
		env   string
		value string
	}{
		{name: "unparseable timeout", env: "HTTP_READ_TIMEOUT", value: "soon"},
		{name: "zero timeout", env: "HTTP_IDLE_TIMEOUT", value: "0s"},
		{name: "unparseable body limit", env: "HTTP_BODY_LIMIT", value: "lots"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.env, tt.value)
			if _, err := LoadConfig(); err == nil {
				t.Errorf("LoadConfig with %s=%q succeeded, want an error", tt.env, tt.value)
			}
		})
	}
}
