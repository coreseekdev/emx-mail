package email

// MailReceiver is the common interface implemented by both IMAPClient and
// POP3Client. It provides a protocol-agnostic way to list, fetch and delete
// emails so that calling code does not need to switch on the protocol.
type MailReceiver interface {
	// FetchMessages lists message envelopes from the server.
	FetchMessages(opts FetchOptions) (*ListResult, error)

	// FetchMessage retrieves a single message by UID / sequence number.
	// For IMAP the folder parameter is honoured; POP3 ignores it.
	FetchMessageByID(folder string, uid uint32) (*Message, error)

	// DeleteMessage removes a message by UID / sequence number.
	// For IMAP the folder and expunge parameters are honoured; POP3 ignores them.
	DeleteMessageByID(folder string, uid uint32, expunge bool) error

	// Close releases the underlying connection, if any.
	Close() error
}
