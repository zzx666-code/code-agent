package llm

type LLMError struct {
	Message string
}

func (e *LLMError) Error() string { return e.Message }

type AuthenticationError struct {
	Message string
}

func (e *AuthenticationError) Error() string { return e.Message }

type RateLimitError struct {
	Message    string
	RetryAfter string
}

func (e *RateLimitError) Error() string { return e.Message }

type NetworkError struct {
	Message string
}

func (e *NetworkError) Error() string { return e.Message }

type ContextTooLongError struct {
	Message string
}

func (e *ContextTooLongError) Error() string { return e.Message }

// ProtocolError indicates that the provider returned a response that could
// not be decoded according to the streaming protocol (for example malformed
// tool-call JSON). It is retryable because the next model response may be
// valid, while still being distinguishable from transport failures.
type ProtocolError struct {
	Message string
}

func (e *ProtocolError) Error() string { return e.Message }

// InvalidToolArgumentsError indicates that a tool call contained malformed or
// otherwise unusable arguments. Keeping this typed lets the agent ask the
// model to repair the call instead of terminating the whole task.
type InvalidToolArgumentsError struct {
	ToolName string
	Message  string
}

func (e *InvalidToolArgumentsError) Error() string { return e.Message }

// ServiceUnavailableError is returned for transient upstream 5xx/overloaded
// responses. It is kept separate from generic LLMError for metrics and retry
// policy decisions.
type ServiceUnavailableError struct {
	Message string
}

func (e *ServiceUnavailableError) Error() string { return e.Message }
