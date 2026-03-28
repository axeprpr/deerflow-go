agent:
	PATH=/usr/local/go/bin:$$PATH go fmt ./pkg/agent/... && PATH=/usr/local/go/bin:$$PATH go build ./pkg/agent/... && echo 'agent OK'
