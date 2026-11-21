// Package transfer implements the FileSwap file framing over a WebRTC
// DataChannel, as specified in docs/protocol.md:
//
//	sender → receiver  text    Meta   {"v":1,"name":…,"size":…,"type":…}
//	sender → receiver  binary  chunks of at most ChunkSize bytes
//	sender → receiver  text    End    {"__end":true,"size":…,"sha256":…}
//	receiver → sender  text    Ack    {"__ack":true,"ok":…,"received":…,"sha256":…}
package transfer

// ChannelLabel is the DataChannel label used for file transfers.
const ChannelLabel = "fileswap-file"

// ChunkSize is the maximum size of a binary chunk (16 KB).
const ChunkSize = 16 * 1024

// ProtocolVersion is the framing version sent in Meta.V.
const ProtocolVersion = 1

// UnknownSize is Meta.Size for content generated while sending (a ZIP of
// several files): the receiver learns the real size from End.Size.
const UnknownSize = -1

// Meta is the first frame of a transfer.
type Meta struct {
	V    int    `json:"v,omitempty"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
	// Approx is an upper bound of the size when Size is UnknownSize, used
	// for the free-space check and progress (bytes of the files zipped).
	Approx int64 `json:"approx,omitempty"`
	// Items is the number of files inside an archive (informative).
	Items int `json:"items,omitempty"`
}

// End is sent after the last chunk. Size is the total sent (required when
// Meta.Size was UnknownSize). SHA256 (lowercase hex) is optional: browsers
// on plain HTTP have no Web Crypto, and they cannot hash a generated ZIP.
type End struct {
	End    bool   `json:"__end"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// Ack is the receiver's answer to End. OK is false when the size or the
// hash does not match; Error then holds one of the Err* codes.
type Ack struct {
	Ack      bool   `json:"__ack"`
	OK       bool   `json:"ok"`
	Received int64  `json:"received"`
	SHA256   string `json:"sha256,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Ack error codes.
const (
	ErrSizeMismatch = "SIZE_MISMATCH"
	ErrHashMismatch = "HASH_MISMATCH"
	ErrWriteFailed  = "WRITE_FAILED"
)
