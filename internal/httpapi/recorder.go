package httpapi

import (
	"cmp"
	"net/http"
)

// statusRecorder records the status and the number of body bytes of a response. Unwrap lets
// http.ResponseController reach the underlying writer, to flush or hijack.
type statusRecorder struct {
	http.ResponseWriter
	// status is the final status, 0 until the response has started.
	status int
	bytes  int64
}

func (w *statusRecorder) WriteHeader(code int) {
	// 1xx headers other than 101 Switching Protocols are informational and precede the final one.
	if w.status == 0 && (code >= http.StatusOK || code == http.StatusSwitchingProtocols) {
		w.status = code
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)

	return n, err
}

// FlushError is what http.ResponseController calls to flush: flushing sends the status, 200 when
// the handler did not set one.
func (w *statusRecorder) FlushError() error {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// started reports whether the status has been sent, after which it can no longer change.
func (w *statusRecorder) started() bool {
	return w.status != 0
}

// finalStatus returns the status of the response, 200 when the handler never set one.
func (w *statusRecorder) finalStatus() int {
	return cmp.Or(w.status, http.StatusOK)
}
