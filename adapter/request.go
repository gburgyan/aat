package adapter

// Request represents an outbound request built by an adapter.
//
// Most of it is shared between the protocols: an HTTP request has a verb and a
// relative path, and a gRPC one puts its full method where the path goes. The
// executor that sends it reads the fields its protocol gives meaning to.
type Request struct {
	// Protocol names how the request is sent. An empty value means HTTP, so a
	// request built before there was a second protocol still reads correctly.
	Protocol string

	// Method is the HTTP verb: GET, POST, PUT, DELETE, PATCH. A gRPC request
	// leaves it empty; its method is part of Path.
	Method string
	// Path is the relative URL path ("/v2/flights/search") for HTTP, and the
	// full method ("shop.v1.Carts/CreateCart") for gRPC.
	Path string
	// Headers are the HTTP headers, or the outgoing metadata of a gRPC call.
	// Metadata keys are lowercased when they are sent.
	Headers map[string]string
	// Body is the request body: the bytes for HTTP, and the request message
	// as JSON for gRPC. It is nil for bodiless methods.
	Body []byte
}

// IsGRPC reports whether the request is sent as a gRPC call.
func (r *Request) IsGRPC() bool { return r.Protocol == ProtocolGRPC }

// NotSentError is an executor's error for a request it could not send: one it
// failed to build, such as a gRPC message with a field its type doesn't have.
// Its message is the wrapped error's. A caller tells it apart from a request
// that was sent and got no response with errors.As.
type NotSentError struct {
	Err error
}

func (e *NotSentError) Error() string { return e.Err.Error() }
func (e *NotSentError) Unwrap() error { return e.Err }
