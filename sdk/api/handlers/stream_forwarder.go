package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"golang.org/x/net/context"
)






func (h *BaseAPIHandler) ExecuteStreamWithPreFTKeepAlive(
	c *gin.Context,
	cliCtx context.Context,
	cliCancel func(error),
	handlerType, modelName string,
	rawJSON []byte,
	alt string,
	keepAliveC <-chan time.Time,
	writeKeepAlive func() bool,
) (<-chan []byte, http.Header, <-chan *interfaces.ErrorMessage, bool) {
	if keepAliveC == nil {
		dataChan, upstreamHeaders, errChan := h.ExecuteStreamWithAuthManager(cliCtx, handlerType, modelName, rawJSON, alt)
		return dataChan, upstreamHeaders, errChan, true
	}

	type streamBootstrapResult struct {
		data    <-chan []byte
		headers http.Header
		errs    <-chan *interfaces.ErrorMessage
	}
	bootstrapResult := make(chan streamBootstrapResult, 1)
	go func() {
		data, headers, errs := h.ExecuteStreamWithAuthManager(cliCtx, handlerType, modelName, rawJSON, alt)
		bootstrapResult <- streamBootstrapResult{data: data, headers: headers, errs: errs}
	}()

	for {
		select {
		case <-c.Request.Context().Done():
			if cliCancel != nil {
				cliCancel(c.Request.Context().Err())
			}
			<-bootstrapResult
			return nil, nil, nil, false
		case result := <-bootstrapResult:
			return result.data, result.headers, result.errs, true
		case <-keepAliveC:
			if writeKeepAlive != nil && !writeKeepAlive() {
				if cliCancel != nil {
					cliCancel(context.Canceled)
				}
				<-bootstrapResult
				return nil, nil, nil, false
			}
		}
	}
}


func PendingStreamError(errs <-chan *interfaces.ErrorMessage) (*interfaces.ErrorMessage, bool) {
	if errs == nil {
		return nil, false
	}
	select {
	case errMsg, ok := <-errs:
		if ok && errMsg != nil {
			return errMsg, true
		}
	default:
	}
	return nil, false
}

type StreamForwardOptions struct {


	KeepAliveInterval *time.Duration


	WriteChunk func(chunk []byte)



	ChunkError func() *interfaces.ErrorMessage


	ChunkDone func() bool


	Flush func() error



	NormalizeTerminalError func(errMsg *interfaces.ErrorMessage) *interfaces.ErrorMessage



	WriteTerminalError func(errMsg *interfaces.ErrorMessage)



	CloseError func() *interfaces.ErrorMessage



	WriteDone func()



	WriteKeepAlive func()
}

func (h *BaseAPIHandler) ForwardStream(c *gin.Context, flusher http.Flusher, cancel func(error), data <-chan []byte, errs <-chan *interfaces.ErrorMessage, opts StreamForwardOptions) {
	if c == nil {
		return
	}
	if cancel == nil {
		return
	}

	flush := func() bool {
		if opts.Flush != nil {
			if errFlush := opts.Flush(); errFlush != nil {
				cancel(errFlush)
				return false
			}
		} else {
			flusher.Flush()
		}
		return true
	}

	writeChunk := opts.WriteChunk
	if writeChunk == nil {
		writeChunk = func([]byte) {}
	}

	writeKeepAlive := opts.WriteKeepAlive
	if writeKeepAlive == nil {
		writeKeepAlive = func() {
			_, _ = c.Writer.Write([]byte(": keep-alive\n\n"))
		}
	}

	keepAliveInterval := StreamingKeepAliveInterval(h.CurrentConfig())
	if opts.KeepAliveInterval != nil {
		keepAliveInterval = *opts.KeepAliveInterval
	}
	keepAliveC, stopKeepAlive, resetKeepAlive := NewStreamingKeepAliveTickerWithReset(keepAliveInterval)
	defer stopKeepAlive()

	var terminalErr *interfaces.ErrorMessage
	handleChunk := func(chunk []byte, ok bool) bool {
		if !ok {

			if terminalErr == nil {
				if errMsg, ok := PendingStreamError(errs); ok {
					terminalErr = errMsg
					if opts.NormalizeTerminalError != nil {
						terminalErr = opts.NormalizeTerminalError(terminalErr)
					}
				}
			}
			if terminalErr == nil && opts.CloseError != nil {
				terminalErr = opts.CloseError()
			}
			if terminalErr != nil {
				if opts.WriteTerminalError != nil {
					opts.WriteTerminalError(terminalErr)
				}
				if !flush() {
					return true
				}
				cancel(terminalErr.Error)
				return true
			}
			if opts.WriteDone != nil {
				opts.WriteDone()
			}
			if !flush() {
				return true
			}
			cancel(nil)
			return true
		}
		beforeSize := c.Writer.Size()
		writeChunk(chunk)
		if c.Writer.Size() > beforeSize {
			resetKeepAlive()
		}
		if !flush() {
			return true
		}
		if opts.ChunkError != nil {
			chunkErr := opts.ChunkError()
			if chunkErr != nil {
				if opts.NormalizeTerminalError != nil {
					chunkErr = opts.NormalizeTerminalError(chunkErr)
				}
				if chunkErr != nil {
					cancel(chunkErr.Error)
				} else {
					cancel(nil)
				}
				return true
			}
		}
		if opts.ChunkDone != nil && opts.ChunkDone() {
			cancel(nil)
			return true
		}
		return false
	}

	for {
		select {
		case <-c.Request.Context().Done():
			cancel(c.Request.Context().Err())
			return
		case chunk, ok := <-data:
			if handleChunk(chunk, ok) {
				return
			}
		case errMsg, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if errMsg != nil {
				terminalErr = errMsg
				if opts.NormalizeTerminalError != nil {
					terminalErr = opts.NormalizeTerminalError(terminalErr)
				}
				if opts.WriteTerminalError != nil {
					opts.WriteTerminalError(terminalErr)
					if !flush() {
						return
					}
				}
			}
			var execErr error
			if terminalErr != nil {
				execErr = terminalErr.Error
			}
			cancel(execErr)
			return
		case <-keepAliveC:
			writeKeepAlive()
			if !flush() {
				return
			}
		}
	}
}
