package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Conversation bounds. A store enforces the first three; the prompt window is
// what one turn reads back.
const (
	// MaxThreadsPerOwner is how many threads one operator may keep.
	MaxThreadsPerOwner = 50
	// MaxMessagesPerThread is how many messages one thread may hold.
	MaxMessagesPerThread = 200
	// MaxMessageBytes bounds one stored message, encoded.
	MaxMessageBytes = 64 << 10
	// MaxMessageTextBytes bounds the text of one stored message.
	MaxMessageTextBytes = 16 << 10
	// MaxThreadTitleBytes bounds a thread title.
	MaxThreadTitleBytes = 120
	// HistoryMaxMessages and HistoryMaxBytes bound the earlier turns a prompt
	// carries. Older messages are left out first.
	HistoryMaxMessages = 20
	HistoryMaxBytes    = 24 << 10

	RoleUser      = "user"
	RoleAssistant = "assistant"

	threadIDPrefix = "ath_"
)

var (
	// ErrThreadNotFound is also the answer for a thread another operator owns.
	ErrThreadNotFound = errors.New("assistant thread was not found")
	ErrThreadLimit    = errors.New("thread limit reached: delete a thread first")
	ErrThreadFull     = errors.New("thread is full: start a new thread")
	ErrThreadInvalid  = errors.New("assistant thread request is not valid")
	// ErrThreadsUnavailable means no conversation store is configured.
	ErrThreadsUnavailable = errors.New("assistant threads are not configured")
)

// ToolRecord is one tool call as it ran: the arguments the server executed
// and the result it returned.
type ToolRecord struct {
	ID        string          `json:"id"`
	Round     int             `json:"round"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// Message is one stored conversation message.
type Message struct {
	Usage     *ReportedTokenUsage `json:"usage,omitempty"`
	Role      string              `json:"role"`
	Text      string              `json:"text"`
	Mode      string              `json:"mode,omitempty"`
	ToolCalls []ToolRecord        `json:"toolCalls,omitempty"`
	// ErrorCode is set on an assistant message whose turn did not finish.
	ErrorCode string    `json:"errorCode,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Thread is one operator's conversation. Owner never leaves the server.
type Thread struct {
	ID           string    `json:"id"`
	Owner        string    `json:"-"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	MessageCount int       `json:"messageCount"`
	Messages     []Message `json:"messages,omitempty"`
}

// ThreadID builds an id from 32 hex characters of entropy.
func ThreadID(entropy string) string { return threadIDPrefix + entropy }

// ValidThreadID reports whether id has the shape the server issues.
func ValidThreadID(id string) bool {
	rest, ok := strings.CutPrefix(id, threadIDPrefix)
	if !ok || len(rest) != 32 {
		return false
	}
	for _, c := range rest {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ValidOwner reports whether owner can key a thread.
func ValidOwner(owner string) bool {
	return owner != "" && owner == strings.TrimSpace(owner) && len(owner) <= 256
}

// ThreadTitle collapses whitespace and cuts the text to the title bound.
func ThreadTitle(text string) string {
	return clip(strings.Join(strings.Fields(text), " "), MaxThreadTitleBytes)
}

// Validate checks one message against the stored bounds.
func (message Message) Validate() error {
	if !message.Usage.Valid() {
		return fmt.Errorf("%w: invalid reported token count", ErrThreadInvalid)
	}
	if message.Role != RoleUser && message.Role != RoleAssistant {
		return fmt.Errorf("%w: unknown role", ErrThreadInvalid)
	}
	if len(message.Text) > MaxMessageTextBytes {
		return fmt.Errorf("%w: message text is over %d bytes", ErrThreadInvalid, MaxMessageTextBytes)
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrThreadInvalid, err)
	}
	if len(encoded) > MaxMessageBytes {
		return fmt.Errorf("%w: message is over %d bytes", ErrThreadInvalid, MaxMessageBytes)
	}
	return nil
}

// Bounded returns the message cut to the stored bounds: the text first, then
// every tool result replaced by a marker, then the tool calls dropped. The
// order is fixed, so the same message always stores the same way.
func (message Message) Bounded() Message {
	message.Text = clip(message.Text, MaxMessageTextBytes)
	if message.Validate() == nil {
		return message
	}
	records := make([]ToolRecord, len(message.ToolCalls))
	for i, record := range message.ToolCalls {
		record.Result = json.RawMessage(`{"truncated":true}`)
		if len(record.Arguments) > 1024 {
			record.Arguments = json.RawMessage(`{"truncated":true}`)
		}
		records[i] = record
	}
	message.ToolCalls = records
	if message.Validate() == nil {
		return message
	}
	message.ToolCalls = nil
	return message
}

// Window returns the newest messages that fit the prompt bounds, oldest first.
// It walks back from the newest message and stops at the first one that does
// not fit, so the window is always one unbroken tail of the thread.
func Window(messages []Message, maxMessages, maxBytes int) []Message {
	start := len(messages)
	used := 0
	for start > 0 && len(messages)-start < maxMessages {
		size := len(messages[start-1].Text) + 64
		if used+size > maxBytes {
			break
		}
		used += size
		start--
	}
	return messages[start:]
}

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}
