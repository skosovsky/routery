# Adapter replay and completion contracts

This is a breaking replacement of the error-only DefaultRetryPolicy APIs in grpc,
mongo, redis, kafka and s3. IsTransientError classifies an error; it never authorizes
repetition. RetryPolicy requires an Evidence callback returning the current attempt
Event and caller-owned attempt.Replay. A nil callback denies repetition. Host evidence
must distinguish read-only/safe duplicate effects, verified server deduplication and
proven NotExecuted outcomes. A request ID or reproducible bytes alone proves none of
these. Mongo additionally rejects transactions. Use execution.Sequence when evidence
can change asynchronously, reconciliation is needed, or physical attempts need accounting.
RetryIf is a local synchronous composition and cannot fence external event changes.

S3 PutRequest has a body-free Input template and an optional BodyFactory. Each factory
call must create an independent full reader, safe for concurrent calls, and transfer
ownership even when returning an error. The adapter closes it on every path. The caller
keeps all other template fields immutable while borrowed. PreparePutRequest consumes and
closes the source body, buffers at most maxBytes+1, and rejects excess bytes; factories
can instead reopen files/streams. Context cancellation is checked between reads; an
uncooperative blocked reader must be interrupted by its owner. Byte replay does not make
PutObject safe with versioning, notifications or unknown effects.

Kafka MessageWriter must complete synchronously with broker acknowledgements and borrow
message headers, keys and values only until return. The adapter snapshots mutable
byte slices for SDK cancellation, which may leave writes active after return. WriterData
remains host-owned until the SDK Completion callback; keep it immutable/alive. The host guarantees this for arbitrary
implementations. The adapter rejects concrete kafka.Writer configurations with Async=true
or RequiredAcks=RequireNone; it cannot detect hidden enqueue-only custom writers. nil is
Acknowledged only under this precondition, never a universally detectable delivery proof.
WriteErrors are aligned by original message index; nil entries are acknowledged, temporary
and permanent errors remain failures with uncertain remote effects, and other errors are
Unknown. Malformed batch lengths remain Unknown. PublishResult preserves original errors;
it does not construct a retry batch. The host selects messages and proves replay safety.

SDK retries are outside this adapter's observation. gRPC service-config/transparent retries,
Mongo retryReads/retryWrites, Redis MaxRetries, Kafka Writer.MaxAttempts and AWS Retryer may
perform multiple wire attempts per method. Disable configurable retries when the host owns
the budget (gRPC WithDisableRetry still permits transparent retries; Mongo retryReads=false
and retryWrites=false; Redis MaxRetries=-1; Kafka MaxAttempts=1; AWS retryer max attempts=1).
Any remaining transparent/client activity is explicitly unobservable, not Coordinator-accounted.
HTTP transports may retry replayable requests on reused connections; SQL drivers may retry
ErrBadConn inside database/sql. Host-wide strict physical budgets need a client/transport
that exposes each actual dispatch; adapter invocation counts cannot guarantee them.

Streaming HTTP/gRPC, SQL/Mongo cursors and S3 downloads retain their existing lifetime
contracts. Headers and CloseSend are not terminal remote completion. RetryUnary/Stream
interceptors deny repeats by default; stream interceptors retry creation only.

RetryIf: a successful call remains success. After a failed call, observed cancellation
wins over the provider error, including the final attempt and a rejecting predicate.
The partial result is retained for cleanup and errors.Join retains the provider error.
Cancellation during the retry wait prevents the next call. Once a failed result has been
closed for a permitted retry, cancellation returns an empty aborted result.
