package helps

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/MoeForge/moego/base64"
	"io"
	"math"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/cache"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	"google.golang.org/protobuf/encoding/protowire"
)

const (

	ConnectFlagData byte = 0x00

	ConnectFlagCompressed byte = 0x01

	ConnectFlagEndStream byte = 0x02


	DevinDefaultBaseURL = "https://server.codeium.com"

	DevinChatPath = "/exa.api_server_pb.ApiServerService/GetChatMessage"


	DevinDefaultClientName = "chisel"

	DevinDefaultClientVersion = "3000.10.21"

	DevinFingerprintHexLen = 732


	DevinDefaultMaxTokens = 128000

	maxConnectFrameSize      = 16 * 1024 * 1024
	maxDecompressedFrameSize = 64 * 1024 * 1024
)


type DevinTool struct {
	Name        string
	Description string
	Parameters  []byte
}


type DevinToolCall struct {
	ID        string
	Name      string
	Arguments string
}


type DevinToolCallDelta struct {
	ID               string
	Name             string
	Arguments        string
	InvalidJSONStr   string
	InvalidJSONErr   string
	IsCustomToolCall bool
}


type DevinImage struct {
	Base64Data string
	MimeType   string
}


type DevinPrompt struct {
	MessageID          string
	Source             int
	Content            string
	Images             []DevinImage
	ToolCalls          []DevinToolCall
	ToolCallID         string
	OriginalToolCallID string
	IsOrphanedTool     bool
	DroppedPart        string
	Thinking           string
	Signature          []byte
	SignatureType      string
}


type DevinUsage struct {
	PromptTokens     int64             `json:"prompt_tokens"`
	CompletionTokens int64             `json:"completion_tokens"`
	CachedTokens     int64             `json:"cached_tokens"`
	CacheWriteTokens int64             `json:"cache_write_tokens,omitempty"`
	StatusCode       uint64            `json:"status_code,omitempty"`
	RequestID        string            `json:"request_id,omitempty"`
	ModelName        string            `json:"model_name,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
}


type DevinFrameResult struct {
	OutputID                string
	Timestamp               uint64
	ContentText             string
	DeltaTokens             uint64
	StopReason              uint64
	ToolCallDeltas          []DevinToolCallDelta
	ThinkingText            string
	DeltaSignature          []byte
	DeltaSignatureType      string
	Latency                 float64
	MessageID               string
	Usage                   *DevinUsage
	ResponseDimensionGroups [][]byte
	UnknownFieldNumbers     []int
}




func GenerateDevinDeviceFingerprint(seed string) string {
	if seed == "" {
		var b [DevinFingerprintHexLen / 2]byte
		if _, err := rand.Read(b[:]); err == nil {
			return hex.EncodeToString(b[:])
		}
		seed = uuid.New().String()
	}
	var sb strings.Builder
	counter := 0
	for sb.Len() < DevinFingerprintHexLen {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", seed, counter)))
		sb.WriteString(hex.EncodeToString(h[:]))
		counter++
	}

	return sb.String()[:DevinFingerprintHexLen]
}



func GenerateDevinSentryTrace() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		u1 := strings.ReplaceAll(uuid.New().String(), "-", "")
		u2 := strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
		return u1 + "-" + u2 + "-1"
	}
	traceID := hex.EncodeToString(b[:16])
	spanID := hex.EncodeToString(b[16:24])
	return traceID + "-" + spanID + "-1"
}

const defaultMaxSessionTurnCounters = 5000

var sessionTurnLRU = cache.NewBoundedLRU[string, *atomic.Uint64](defaultMaxSessionTurnCounters, nil)





func NextDevinSessionTurnIndex(sessionID string) int {
	cleanID := strings.TrimSpace(sessionID)
	if cleanID == "" {
		return 0
	}

	counter := sessionTurnLRU.GetOrAdd(cleanID, func() *atomic.Uint64 {
		return &atomic.Uint64{}
	})
	return int(counter.Add(1) - 1)
}


func ResetDevinSessionTurnIndex(sessionID string) {
	sessionTurnLRU.Delete(strings.TrimSpace(sessionID))
}



func WrapConnectEnvelope(protoBytes []byte) []byte {
	return WrapConnectEnvelopeWithFlag(ConnectFlagData, protoBytes)
}


func WrapConnectEnvelopeWithFlag(flag byte, protoBytes []byte) []byte {
	header := make([]byte, 5, 5+len(protoBytes))
	header[0] = flag
	binary.BigEndian.PutUint32(header[1:5], uint32(len(protoBytes)))
	return append(header, protoBytes...)
}


func ReadConnectFrame(r io.Reader) (flag byte, payload []byte, err error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	flag = header[0]
	if flag != ConnectFlagData && flag != ConnectFlagCompressed && flag != ConnectFlagEndStream && flag != (ConnectFlagCompressed|ConnectFlagEndStream) {
		return flag, nil, fmt.Errorf("invalid connect frame flag: 0x%02x", flag)
	}
	length := binary.BigEndian.Uint32(header[1:5])
	if length > maxConnectFrameSize {
		return flag, nil, fmt.Errorf("connect frame length %d exceeds maximum limit (%d)", length, maxConnectFrameSize)
	}

	payload = make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}

	if flag&ConnectFlagCompressed != 0 {
		gz, errGz := gzip.NewReader(bytes.NewReader(payload))
		if errGz != nil {
			return flag, nil, fmt.Errorf("decompress gzip connect frame: %w", errGz)
		}
		defer func() { _ = gz.Close() }()

		initCap := int(length) * 4
		if initCap > 4*1024*1024 {
			initCap = 4 * 1024 * 1024
		} else if initCap < 4096 {
			initCap = 4096
		}
		decompBuf := bytes.NewBuffer(make([]byte, 0, initCap))
		limitedReader := io.LimitReader(gz, maxDecompressedFrameSize+1)
		if _, errRead := decompBuf.ReadFrom(limitedReader); errRead != nil {
			return flag, nil, fmt.Errorf("read decompressed connect frame: %w", errRead)
		}
		if decompBuf.Len() > maxDecompressedFrameSize {
			return flag, nil, fmt.Errorf("decompressed frame size exceeds maximum limit (%d)", maxDecompressedFrameSize)
		}
		payload = decompBuf.Bytes()
	}

	return flag, payload, nil
}


func BuildDevinClientMetadataBytes(sessionToken, deviceSeed, osName string) []byte {
	if osName == "" {
		osName = runtime.GOOS
	}
	deviceFingerprint := GenerateDevinDeviceFingerprint(deviceSeed)

	var f1Bytes []byte
	f1Bytes = protowire.AppendTag(f1Bytes, 1, protowire.BytesType)
	f1Bytes = protowire.AppendString(f1Bytes, DevinDefaultClientName)

	f1Bytes = protowire.AppendTag(f1Bytes, 2, protowire.BytesType)
	f1Bytes = protowire.AppendString(f1Bytes, DevinDefaultClientVersion)

	f1Bytes = protowire.AppendTag(f1Bytes, 3, protowire.BytesType)
	f1Bytes = protowire.AppendString(f1Bytes, sessionToken)

	f1Bytes = protowire.AppendTag(f1Bytes, 4, protowire.BytesType)
	f1Bytes = protowire.AppendString(f1Bytes, "en")

	f1Bytes = protowire.AppendTag(f1Bytes, 5, protowire.BytesType)
	f1Bytes = protowire.AppendString(f1Bytes, osName)

	f1Bytes = protowire.AppendTag(f1Bytes, 7, protowire.BytesType)
	f1Bytes = protowire.AppendString(f1Bytes, DevinDefaultClientVersion)

	f1Bytes = protowire.AppendTag(f1Bytes, 12, protowire.BytesType)
	f1Bytes = protowire.AppendString(f1Bytes, DevinDefaultClientName)

	f1Bytes = protowire.AppendTag(f1Bytes, 31, protowire.BytesType)
	f1Bytes = protowire.AppendString(f1Bytes, deviceFingerprint)
	return f1Bytes
}


func BuildDevinGetChatMessageRequest(
	sessionToken string,
	deviceSeed string,
	chatModelUID string,
	systemPrompt string,
	prompts []DevinPrompt,
	tools []DevinTool,
	temperature *float64,
	maxTokens int,
	sessionID string,
	cascadeID string,
	matcher *SensitiveWordMatcher,
) []byte {
	if maxTokens <= 0 {
		maxTokens = DevinDefaultMaxTokens
	}
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	if cascadeID == "" {
		cascadeID = sessionID
	}
	osName := runtime.GOOS

	estimatedSize := 4096 + len(systemPrompt)
	for _, p := range prompts {
		estimatedSize += 256 + len(p.Content)
	}
	reqBytes := make([]byte, 0, estimatedSize)


	f1Bytes := BuildDevinClientMetadataBytes(sessionToken, deviceSeed, osName)
	reqBytes = protowire.AppendTag(reqBytes, 1, protowire.BytesType)
	reqBytes = protowire.AppendBytes(reqBytes, f1Bytes)


	if systemPrompt != "" {
		sanitized := SanitizeDevinSystemPrompt(systemPrompt, matcher)
		if sanitized != "" {
			reqBytes = protowire.AppendTag(reqBytes, 2, protowire.BytesType)
			reqBytes = protowire.AppendString(reqBytes, sanitized)
		}
	}


	for _, p := range prompts {
		var pBytes []byte

		msgID := p.MessageID
		if msgID == "" {
			msgID = uuid.New().String()
		}
		pBytes = protowire.AppendTag(pBytes, 1, protowire.BytesType)
		pBytes = protowire.AppendString(pBytes, msgID)

		source := p.Source
		if source <= 0 {
			source = 1
		}
		pBytes = protowire.AppendTag(pBytes, 2, protowire.VarintType)
		pBytes = protowire.AppendVarint(pBytes, uint64(source))

		content := p.Content
		pBytes = protowire.AppendTag(pBytes, 3, protowire.BytesType)
		pBytes = protowire.AppendString(pBytes, content)

		for _, tc := range p.ToolCalls {
			var tcBytes []byte
			if tc.ID != "" {
				tcBytes = protowire.AppendTag(tcBytes, 1, protowire.BytesType)
				tcBytes = protowire.AppendString(tcBytes, tc.ID)
			}
			if tc.Name != "" {
				tcBytes = protowire.AppendTag(tcBytes, 2, protowire.BytesType)
				tcBytes = protowire.AppendString(tcBytes, tc.Name)
			}
			if tc.Arguments != "" {
				tcBytes = protowire.AppendTag(tcBytes, 3, protowire.BytesType)
				tcBytes = protowire.AppendString(tcBytes, tc.Arguments)
			}
			pBytes = protowire.AppendTag(pBytes, 6, protowire.BytesType)
			pBytes = protowire.AppendBytes(pBytes, tcBytes)
		}

		if p.ToolCallID != "" {
			pBytes = protowire.AppendTag(pBytes, 7, protowire.BytesType)
			pBytes = protowire.AppendString(pBytes, p.ToolCallID)
		}

		for _, img := range p.Images {
			data := strings.TrimSpace(img.Base64Data)
			if data == "" {
				continue
			}
			var imgBytes []byte
			imgBytes = protowire.AppendTag(imgBytes, 1, protowire.BytesType)
			imgBytes = protowire.AppendString(imgBytes, data)

			mime := strings.TrimSpace(img.MimeType)
			if mime == "" {
				mime = "image/png"
			}
			imgBytes = protowire.AppendTag(imgBytes, 2, protowire.BytesType)
			imgBytes = protowire.AppendString(imgBytes, mime)

			pBytes = protowire.AppendTag(pBytes, 10, protowire.BytesType)
			pBytes = protowire.AppendBytes(pBytes, imgBytes)
		}

		if p.Thinking != "" {
			pBytes = protowire.AppendTag(pBytes, 11, protowire.BytesType)
			pBytes = protowire.AppendString(pBytes, p.Thinking)
		}

		if len(p.Signature) > 0 {
			pBytes = protowire.AppendTag(pBytes, 12, protowire.BytesType)
			pBytes = protowire.AppendBytes(pBytes, p.Signature)
		}

		if p.SignatureType != "" {
			pBytes = protowire.AppendTag(pBytes, 18, protowire.BytesType)
			pBytes = protowire.AppendString(pBytes, p.SignatureType)
		}

		reqBytes = protowire.AppendTag(reqBytes, 3, protowire.BytesType)
		reqBytes = protowire.AppendBytes(reqBytes, pBytes)
	}


	reqBytes = protowire.AppendTag(reqBytes, 7, protowire.VarintType)
	reqBytes = protowire.AppendVarint(reqBytes, 5)


	var f8Bytes []byte
	f8Bytes = protowire.AppendTag(f8Bytes, 1, protowire.VarintType)
	f8Bytes = protowire.AppendVarint(f8Bytes, 1)

	f8Bytes = protowire.AppendTag(f8Bytes, 2, protowire.VarintType)
	f8Bytes = protowire.AppendVarint(f8Bytes, uint64(maxTokens))

	f8Bytes = protowire.AppendTag(f8Bytes, 3, protowire.VarintType)
	f8Bytes = protowire.AppendVarint(f8Bytes, 400)

	tempVal := 1.0
	if temperature != nil {
		tempVal = *temperature
	}
	f8Bytes = protowire.AppendTag(f8Bytes, 5, protowire.Fixed64Type)
	f8Bytes = protowire.AppendFixed64(f8Bytes, math.Float64bits(tempVal))

	f8Bytes = protowire.AppendTag(f8Bytes, 7, protowire.VarintType)
	f8Bytes = protowire.AppendVarint(f8Bytes, 40)

	f8Bytes = protowire.AppendTag(f8Bytes, 8, protowire.Fixed64Type)
	f8Bytes = protowire.AppendFixed64(f8Bytes, math.Float64bits(float64(float32(0.95))))

	reqBytes = protowire.AppendTag(reqBytes, 8, protowire.BytesType)
	reqBytes = protowire.AppendBytes(reqBytes, f8Bytes)


	for _, tool := range tools {
		if tool.Name == "" || translatorcommon.IsDevinCodexAppAutomationUpdate("", tool.Name) {
			continue
		}
		var tBytes []byte
		if tool.Name != "" {
			tBytes = protowire.AppendTag(tBytes, 1, protowire.BytesType)
			tBytes = protowire.AppendString(tBytes, tool.Name)
		}
		desc := tool.Description



		if strings.Contains(desc, "Takes a task_id parameter identifying the task") {
			desc = strings.ReplaceAll(desc, "Takes a task_id parameter identifying the task", "Takes a taskId parameter identifying the task")
		}
		desc = translatorcommon.SanitizeDevinToolDescription(tool.Name, desc)
		if desc != "" {
			tBytes = protowire.AppendTag(tBytes, 2, protowire.BytesType)
			tBytes = protowire.AppendString(tBytes, desc)
		}
		if len(tool.Parameters) > 0 {
			tBytes = protowire.AppendTag(tBytes, 3, protowire.BytesType)
			tBytes = protowire.AppendBytes(tBytes, tool.Parameters)
		}
		reqBytes = protowire.AppendTag(reqBytes, 10, protowire.BytesType)
		reqBytes = protowire.AppendBytes(reqBytes, tBytes)
	}







	turnIndex := NextDevinSessionTurnIndex(sessionID)

	var f15Bytes []byte
	f15Bytes = protowire.AppendTag(f15Bytes, 1, protowire.BytesType)
	f15Bytes = protowire.AppendString(f15Bytes, sessionID)

	if turnIndex > 0 {
		f15Bytes = protowire.AppendTag(f15Bytes, 2, protowire.VarintType)
		f15Bytes = protowire.AppendVarint(f15Bytes, uint64(turnIndex))
	}

	f15Bytes = protowire.AppendTag(f15Bytes, 3, protowire.VarintType)
	f15Bytes = protowire.AppendVarint(f15Bytes, 4)


	if len(prompts) > 0 && prompts[len(prompts)-1].Source == 1 {
		if turnIndex == 0 || len(prompts) < 2 || prompts[len(prompts)-2].Source != 1 {
			f15Bytes = protowire.AppendTag(f15Bytes, 4, protowire.VarintType)
			f15Bytes = protowire.AppendVarint(f15Bytes, 14)
		}
	}

	reqBytes = protowire.AppendTag(reqBytes, 15, protowire.BytesType)
	reqBytes = protowire.AppendBytes(reqBytes, f15Bytes)


	reqBytes = protowire.AppendTag(reqBytes, 16, protowire.BytesType)
	reqBytes = protowire.AppendString(reqBytes, cascadeID)


	reqBytes = protowire.AppendTag(reqBytes, 20, protowire.VarintType)
	reqBytes = protowire.AppendVarint(reqBytes, 1)


	reqBytes = protowire.AppendTag(reqBytes, 21, protowire.BytesType)
	reqBytes = protowire.AppendString(reqBytes, chatModelUID)

	return reqBytes
}


func ParseDevinFrame(payload []byte) (DevinFrameResult, error) {
	var res DevinFrameResult
	var textParts []string
	var thinkingParts []string

	pos := 0
	for pos < len(payload) {
		num, typ, n := protowire.ConsumeTag(payload[pos:])
		if n <= 0 {
			return res, fmt.Errorf("consume tag error at offset %d: %w", pos, protowire.ParseError(n))
		}
		pos += n

		switch typ {
		case protowire.VarintType:
			v, vn := protowire.ConsumeVarint(payload[pos:])
			if vn <= 0 {
				return res, fmt.Errorf("consume varint error at offset %d: %w", pos, protowire.ParseError(vn))
			}
			pos += vn
			switch num {
			case 2:
				res.Timestamp = v
			case 4:
				res.DeltaTokens = v
			case 5:
				res.StopReason = v
			}

		case protowire.Fixed64Type:
			v, fn := protowire.ConsumeFixed64(payload[pos:])
			if fn <= 0 {
				return res, fmt.Errorf("consume fixed64 error at offset %d: %w", pos, protowire.ParseError(fn))
			}
			pos += fn
			if num == 12 {
				res.Latency = math.Float64frombits(v)
			}

		case protowire.Fixed32Type:
			_, fn := protowire.ConsumeFixed32(payload[pos:])
			if fn <= 0 {
				return res, fmt.Errorf("consume fixed32 error at offset %d: %w", pos, protowire.ParseError(fn))
			}
			pos += fn

		case protowire.BytesType:
			val, bn := protowire.ConsumeBytes(payload[pos:])
			if bn <= 0 {
				return res, fmt.Errorf("consume bytes error at offset %d: %w", pos, protowire.ParseError(bn))
			}
			pos += bn

			switch num {
			case 1:
				res.OutputID = string(val)
			case 2:
				res.Timestamp = parseDevinTimestamp(val)
			case 3:
				textParts = append(textParts, string(val))
			case 6:
				if tc, err := parseDevinToolCallDelta(val); err == nil {
					res.ToolCallDeltas = append(res.ToolCallDeltas, tc)
				}
			case 7:
				res.Usage = parseDevinUsageField(val)
			case 9:
				thinkingParts = append(thinkingParts, string(val))
			case 10:
				res.DeltaSignature = append(res.DeltaSignature, val...)
			case 17:
				res.MessageID = string(val)
			case 21:
				res.DeltaSignatureType = string(val)
			case 28:
				res.ResponseDimensionGroups = append(res.ResponseDimensionGroups, val)
			default:
				res.UnknownFieldNumbers = append(res.UnknownFieldNumbers, int(num))
			}

		default:
			return res, fmt.Errorf("unsupported wire type %d at offset %d", typ, pos)
		}
	}

	if len(textParts) > 0 {
		res.ContentText = strings.Join(textParts, "")
	}
	if len(thinkingParts) > 0 {
		res.ThinkingText = strings.Join(thinkingParts, "")
	}

	return res, nil
}



func SanitizeDevinSystemPrompt(prompt string, matcher *SensitiveWordMatcher) string {
	if prompt == "" {
		return ""
	}
	normalized := strings.ReplaceAll(prompt, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if util.IsClaudeCodeAttributionSystemText(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "You are Claude Code") {
			continue
		}
		if strings.Contains(trimmed, "authorized security testing") || strings.Contains(trimmed, "destructive techniques, DoS attacks") {
			continue
		}
		if strings.Contains(trimmed, "Claude Code is available as a CLI") {
			continue
		}
		if strings.Contains(trimmed, "Fast mode for Claude Code") {
			continue
		}
		if strings.Contains(trimmed, "Codex refers to the open-source agentic coding interface") {
			continue
		}
		if strings.Contains(trimmed, "- Don’t output ANSI escape codes directly — the CLI renderer applies them.") {
			continue
		}
		kept = append(kept, line)
	}
	res := strings.Join(kept, "\n")
	if matcher != nil && res != "" {
		return matcher.SanitizeDevinText(res)
	}
	return strings.TrimSpace(res)
}

func parseDevinToolCallDelta(data []byte) (DevinToolCallDelta, error) {
	var tc DevinToolCallDelta
	pos := 0
	for pos < len(data) {
		num, typ, n := protowire.ConsumeTag(data[pos:])
		if n <= 0 {
			return tc, protowire.ParseError(n)
		}
		pos += n

		switch typ {
		case protowire.VarintType:
			v, vn := protowire.ConsumeVarint(data[pos:])
			if vn <= 0 {
				return tc, protowire.ParseError(vn)
			}
			pos += vn
			if num == 6 {
				tc.IsCustomToolCall = (v != 0)
			}
		case protowire.BytesType:
			val, bn := protowire.ConsumeBytes(data[pos:])
			if bn <= 0 {
				return tc, protowire.ParseError(bn)
			}
			pos += bn
			switch num {
			case 1:
				tc.ID = string(val)
			case 2:
				tc.Name = string(val)
			case 3:
				tc.Arguments = string(val)
			case 4:
				tc.InvalidJSONStr = string(val)
			case 5:
				tc.InvalidJSONErr = string(val)
			}
		default:
			nSkip := protowire.ConsumeFieldValue(num, typ, data[pos:])
			if nSkip <= 0 {
				return tc, protowire.ParseError(nSkip)
			}
			pos += nSkip
		}
	}
	return tc, nil
}

func parseDevinTimestamp(data []byte) uint64 {
	pos := 0
	var secs uint64
	for pos < len(data) {
		num, typ, n := protowire.ConsumeTag(data[pos:])
		if n <= 0 {
			break
		}
		pos += n
		if typ == protowire.VarintType {
			v, vn := protowire.ConsumeVarint(data[pos:])
			if vn <= 0 {
				break
			}
			pos += vn
			if num == 1 {
				secs = v
			}
		} else {
			break
		}
	}
	return secs
}




func parseDevinHeaderField(data []byte) (string, string) {
	var key, val string
	pos := 0
	for pos < len(data) {
		num, typ, n := protowire.ConsumeTag(data[pos:])
		if n <= 0 {
			break
		}
		pos += n
		switch typ {
		case protowire.BytesType:
			b, bn := protowire.ConsumeBytes(data[pos:])
			if bn <= 0 {
				return key, val
			}
			pos += bn
			switch num {
			case 1:
				key = string(b)
			case 2:
				val = string(b)
			}
		default:
			nSkip := protowire.ConsumeFieldValue(num, typ, data[pos:])
			if nSkip <= 0 {
				return key, val
			}
			pos += nSkip
		}
	}
	return key, val
}

func parseDevinUsageField(data []byte) *DevinUsage {
	u := &DevinUsage{}
	pos := 0
	for pos < len(data) {
		num, typ, n := protowire.ConsumeTag(data[pos:])
		if n <= 0 {
			break
		}
		pos += n

		switch typ {
		case protowire.VarintType:
			v, vn := protowire.ConsumeVarint(data[pos:])
			if vn <= 0 {
				return u
			}
			pos += vn
			switch num {
			case 2:
				u.PromptTokens += int64(v)
			case 3:
				u.CompletionTokens = int64(v)
			case 4:
				u.CacheWriteTokens += int64(v)
			case 5:
				u.CachedTokens = int64(v)
			case 6:
				u.StatusCode = v
			}
		case protowire.BytesType:
			val, bn := protowire.ConsumeBytes(data[pos:])
			if bn <= 0 {
				return u
			}
			pos += bn
			switch num {
			case 8:
				k, v := parseDevinHeaderField(val)
				if k != "" {
					if u.Headers == nil {
						u.Headers = make(map[string]string)
					}
					u.Headers[k] = v
					if (strings.EqualFold(k, "x-request-id") || strings.EqualFold(k, "request-id")) && v != "" {
						u.RequestID = v
					}
				} else if len(val) > 0 && isPrintableASCII(val) && u.RequestID == "" {
					u.RequestID = string(val)
				}
			case 9:
				u.ModelName = string(val)
			}
		case protowire.Fixed64Type:
			_, fn := protowire.ConsumeFixed64(data[pos:])
			if fn <= 0 {
				return u
			}
			pos += fn
		case protowire.Fixed32Type:
			_, fn := protowire.ConsumeFixed32(data[pos:])
			if fn <= 0 {
				return u
			}
			pos += fn
		default:
			nSkip := protowire.ConsumeFieldValue(num, typ, data[pos:])
			if nSkip <= 0 {
				return u
			}
			pos += nSkip
		}
	}
	return u
}




func ParseDevinResponseDimensionGroups(groups ...[]byte) (promptTokens, completionTokens, cachedTokens int64, found bool) {
	for _, gBytes := range groups {
		if len(gBytes) == 0 {
			continue
		}

		if num, typ, n := protowire.ConsumeTag(gBytes); n > 0 && num == 28 && typ == protowire.BytesType {
			if inner, bn := protowire.ConsumeBytes(gBytes[n:]); bn > 0 {
				gBytes = inner
			}
		}

		gPos := 0
		var title string
		type metricItem struct {
			key string
			val float32
		}
		var metrics []metricItem
		for gPos < len(gBytes) {
			gNum, gTyp, gn := protowire.ConsumeTag(gBytes[gPos:])
			if gn <= 0 {
				break
			}
			gPos += gn
			if gTyp != protowire.BytesType {
				gSkip := protowire.ConsumeFieldValue(gNum, gTyp, gBytes[gPos:])
				if gSkip <= 0 {
					break
				}
				gPos += gSkip
				continue
			}
			gb, gbn := protowire.ConsumeBytes(gBytes[gPos:])
			if gbn <= 0 {
				break
			}
			gPos += gbn
			if gNum == 1 {
				title = string(gb)
			} else if gNum == 2 {
				mPos := 0
				var mKey string
				var mVal float32
				for mPos < len(gb) {
					mNum, mTyp, mn := protowire.ConsumeTag(gb[mPos:])
					if mn <= 0 {
						break
					}
					mPos += mn
					if mTyp != protowire.BytesType {
						mSkip := protowire.ConsumeFieldValue(mNum, mTyp, gb[mPos:])
						if mSkip <= 0 {
							break
						}
						mPos += mSkip
						continue
					}
					mb, mbn := protowire.ConsumeBytes(gb[mPos:])
					if mbn <= 0 {
						break
					}
					mPos += mbn
					if mNum == 5 {
						mKey = string(mb)
					} else if mNum == 4 {
						dPos := 0
						for dPos < len(mb) {
							dNum, dTyp, dn := protowire.ConsumeTag(mb[dPos:])
							if dn <= 0 {
								break
							}
							dPos += dn
							if dTyp == protowire.Fixed32Type {
								dv, dfn := protowire.ConsumeFixed32(mb[dPos:])
								if dfn <= 0 {
									break
								}
								dPos += dfn
								if dNum == 2 {
									mVal = math.Float32frombits(dv)
								}
							} else {
								dSkip := protowire.ConsumeFieldValue(dNum, dTyp, mb[dPos:])
								if dSkip <= 0 {
									break
								}
								dPos += dSkip
							}
						}
					}
				}
				if mKey != "" {
					metrics = append(metrics, metricItem{key: mKey, val: mVal})
				}
			}
		}

		if strings.EqualFold(title, "Token Usage") {
			for _, m := range metrics {
				switch m.key {
				case "input_tokens":
					promptTokens = int64(m.val)
					found = true
				case "output_tokens":
					completionTokens = int64(m.val)
					found = true
				case "cached_input_tokens":
					cachedTokens = int64(m.val)
					found = true
				}
			}
			if found {
				return promptTokens, completionTokens, cachedTokens, true
			}
		}
	}
	return promptTokens, completionTokens, cachedTokens, found
}


func ParseDevinTrailerError(payload []byte) (statusCode int, err error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
		return 0, nil
	}
	var trailer struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if errUnmarshal := json.Unmarshal(trimmed, &trailer); errUnmarshal != nil || trailer.Error == nil {
		return 0, nil
	}

	codeStr := strings.ToLower(trailer.Error.Code)
	msgLower := strings.ToLower(trailer.Error.Message)

	httpCode := http.StatusBadGateway
	switch codeStr {
	case "invalid_argument":
		if strings.Contains(msgLower, "internal error") {
			httpCode = http.StatusBadGateway
		} else {
			httpCode = http.StatusBadRequest
		}
	case "internal":
		httpCode = http.StatusBadGateway
	case "unauthenticated":
		httpCode = http.StatusUnauthorized
	case "permission_denied":
		if strings.Contains(msgLower, "high demand") {
			httpCode = http.StatusTooManyRequests
		} else {
			httpCode = http.StatusForbidden
		}
	case "resource_exhausted":
		httpCode = http.StatusTooManyRequests
	case "unavailable":
		httpCode = http.StatusServiceUnavailable
	case "canceled":
		httpCode = 499
	case "deadline_exceeded":
		httpCode = http.StatusGatewayTimeout
	case "failed_precondition":
		if strings.Contains(msgLower, "quota") ||
			strings.Contains(msgLower, "credit") ||
			strings.Contains(msgLower, "acu") ||
			strings.Contains(msgLower, "exhausted") ||
			strings.Contains(msgLower, "limit") {
			httpCode = http.StatusTooManyRequests
		} else {
			httpCode = http.StatusBadRequest
		}
	}

	return httpCode, fmt.Errorf("devin upstream error (%s): %s", trailer.Error.Code, trailer.Error.Message)
}


type UTF8SplitBuffer struct {
	remainder []byte
}


func (b *UTF8SplitBuffer) Feed(chunk []byte) string {
	combined := append(b.remainder, chunk...)
	b.remainder = nil

	if len(combined) == 0 {
		return ""
	}

	validUntil := 0
	for validUntil < len(combined) {
		r, size := utf8.DecodeRune(combined[validUntil:])
		if r == utf8.RuneError && size == 1 {
			trailingLen := len(combined) - validUntil
			if trailingLen < utf8.UTFMax && !utf8.FullRune(combined[validUntil:]) {
				break
			}
			validUntil++
			continue
		}
		validUntil += size
	}

	validBytes := combined[:validUntil]
	b.remainder = append(b.remainder, combined[validUntil:]...)

	return string(validBytes)
}


type DevinUpstreamRequestLog struct {
	Model        string               `json:"model"`
	SessionID    string               `json:"session_id,omitempty"`
	CascadeID    string               `json:"cascade_id,omitempty"`
	SystemPrompt string               `json:"system_prompt,omitempty"`
	Temperature  *float64             `json:"temperature,omitempty"`
	MaxTokens    int                  `json:"max_tokens,omitempty"`
	Prompts      []DevinPromptLogItem `json:"prompts,omitempty"`
	Tools        []DevinToolLogItem   `json:"tools,omitempty"`
}


type DevinPromptLogItem struct {
	ID            string              `json:"id,omitempty"`
	Source        int                 `json:"source"`
	Role          string              `json:"role,omitempty"`
	Content       string              `json:"content,omitempty"`
	Thinking      string              `json:"thinking,omitempty"`
	Signature     string              `json:"signature,omitempty"`
	SignatureType string              `json:"signature_type,omitempty"`
	ToolCalls     []DevinToolCall     `json:"tool_calls,omitempty"`
	ToolCallID    string              `json:"tool_call_id,omitempty"`
	Images        []DevinImageLogItem `json:"images,omitempty"`
}


type DevinImageLogItem struct {
	MimeType string `json:"mime_type"`
	DataLen  int    `json:"data_len"`
}


type DevinToolLogItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func formatSignatureForLog(sig []byte) string {
	if len(sig) == 0 {
		return ""
	}
	if bytes.HasPrefix(sig, []byte("sealed.v1.")) {
		return string(sig)
	}
	if utf8.Valid(sig) && isPrintableASCII(sig) {
		return string(sig)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func isPrintableASCII(b []byte) bool {
	for _, c := range b {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}



func BuildDevinUpstreamLogBody(
	interactionsPayload []byte,
	isInteractionsSource bool,
	chatModelUID string,
	systemPrompt string,
	prompts []DevinPrompt,
	tools []DevinTool,
	temp *float64,
	maxTokens int,
	sessionID string,
	cascadeID string,
) []byte {
	var promptItems []DevinPromptLogItem
	for _, p := range prompts {
		role := "user"
		switch p.Source {
		case 2:
			role = "assistant"
		case 4:
			role = "tool"
		}
		var imgItems []DevinImageLogItem
		for _, img := range p.Images {
			imgItems = append(imgItems, DevinImageLogItem{
				MimeType: img.MimeType,
				DataLen:  len(img.Base64Data),
			})
		}
		promptItems = append(promptItems, DevinPromptLogItem{
			ID:            p.MessageID,
			Source:        p.Source,
			Role:          role,
			Content:       p.Content,
			Thinking:      p.Thinking,
			Signature:     formatSignatureForLog(p.Signature),
			SignatureType: p.SignatureType,
			ToolCalls:     p.ToolCalls,
			ToolCallID:    p.ToolCallID,
			Images:        imgItems,
		})
	}

	var toolItems []DevinToolLogItem
	for _, t := range tools {
		if t.Name == "" || translatorcommon.IsDevinCodexAppAutomationUpdate("", t.Name) {
			continue
		}
		desc := t.Description
		if strings.Contains(desc, "Takes a task_id parameter identifying the task") {
			desc = strings.ReplaceAll(desc, "Takes a task_id parameter identifying the task", "Takes a taskId parameter identifying the task")
		}
		desc = translatorcommon.SanitizeDevinToolDescription(t.Name, desc)
		var params json.RawMessage
		if len(t.Parameters) > 0 && json.Valid(t.Parameters) {
			params = json.RawMessage(t.Parameters)
		}
		toolItems = append(toolItems, DevinToolLogItem{
			Name:        t.Name,
			Description: desc,
			Parameters:  params,
		})
	}

	devinReq := DevinUpstreamRequestLog{
		Model:        chatModelUID,
		SessionID:    sessionID,
		CascadeID:    cascadeID,
		SystemPrompt: systemPrompt,
		Temperature:  temp,
		MaxTokens:    maxTokens,
		Prompts:      promptItems,
		Tools:        toolItems,
	}

	devinReqJSON, errDevin := json.MarshalIndent(devinReq, "", "  ")
	if errDevin != nil {
		devinReqJSON = []byte(fmt.Sprintf(`{"model": %q}`, chatModelUID))
	}

	var buf bytes.Buffer
	if !isInteractionsSource && len(interactionsPayload) > 0 {
		buf.WriteString("=== INTERMEDIATE INTERACTIONS ===\n")
		var prettyInteractions bytes.Buffer
		if err := json.Indent(&prettyInteractions, interactionsPayload, "", "  "); err == nil {
			buf.Write(prettyInteractions.Bytes())
		} else {
			buf.Write(interactionsPayload)
		}
		buf.WriteString("\n\n=== DEVIN UPSTREAM REQUEST ===\n")
		buf.Write(devinReqJSON)
	} else {
		buf.Write(devinReqJSON)
	}

	return buf.Bytes()
}


type DevinUpstreamResponseLog struct {
	Status                string                      `json:"status,omitempty"`
	FramesCount           int                         `json:"frames_count"`
	Content               string                      `json:"content,omitempty"`
	Thinking              string                      `json:"thinking,omitempty"`
	Signature             string                      `json:"signature,omitempty"`
	SignatureType         string                      `json:"signature_type,omitempty"`
	ToolCalls             []DevinToolCall             `json:"tool_calls,omitempty"`
	Usage                 *DevinUsage                 `json:"usage,omitempty"`
	UnknownFields         []int                       `json:"unknown_fields,omitempty"`
	ApplyPatchDiagnostics *DevinApplyPatchDiagnostics `json:"-"`
}



func BuildDevinUpstreamResponseLogBody(respLog *DevinUpstreamResponseLog, interactionsJSON []byte) []byte {
	var buf bytes.Buffer
	if respLog != nil {
		if len(respLog.Signature) > 0 {
			respLog.Signature = formatSignatureForLog([]byte(respLog.Signature))
		}
		respJSON, err := json.MarshalIndent(respLog, "", "  ")
		if err == nil && len(respJSON) > 0 {
			buf.WriteString("=== DEVIN UPSTREAM RESPONSE ===\n")
			buf.Write(respJSON)
			buf.WriteString("\n\n")
		}
	}
	if len(interactionsJSON) > 0 {
		buf.WriteString("=== INTERMEDIATE INTERACTIONS ===\n")
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, interactionsJSON, "", "  "); err == nil {
			buf.Write(pretty.Bytes())
		} else {
			buf.Write(interactionsJSON)
		}
	}
	return buf.Bytes()
}
