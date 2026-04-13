package harnessruntime

import (
	"io"
	"net/http"
)

func NewHTTPRemoteWorkerHandler(transport WorkerTransport, protocol RemoteWorkerProtocol) http.Handler {
	if transport == nil {
		transport = NewDirectWorkerTransport(nil, DispatchEnvelopeCodec{})
	}
	protocol = defaultRemoteWorkerProtocol(protocol, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		body, err := ioReadAll(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		envelope, err := protocol.DecodeRequest(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := transport.Submit(r.Context(), envelope)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		payload, err := protocol.EncodeResponse(result)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	})
}

func ioReadAll(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}
