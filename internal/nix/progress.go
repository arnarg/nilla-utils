package nix

import (
	"bufio"
	"bytes"
	"context"
	"io"

	"github.com/valyala/fastjson"
)

const (
	protoActionStart   string = "start"
	protoActionStop    string = "stop"
	protoActionResult  string = "result"
	protoActionMessage string = "msg"
)

const (
	protoEventTypeUnknown       = 0
	protoEventTypeCopyPath      = 100
	protoEventTypeFileTransfer  = 101
	protoEventTypeRealise       = 102
	protoEventTypeCopyPaths     = 103
	protoEventTypeBuilds        = 104
	protoEventTypeBuild         = 105
	protoEventTypeOptimiseStore = 106
	protoEventTypeVerifyPaths   = 107
	protoEventTypeSubstitute    = 108
	protoEventTypeQueryPathInfo = 109
	protoEventTypePostBuildHook = 110
	protoEventTypeBuildWaiting  = 111
	protoEventTypeFetchTree     = 112
)

const (
	protoResultTypeFileLinked       = 100
	protoResultTypeBuildLogLine     = 101
	protoResultTypeUntrustedPath    = 102
	protoResultTypeCorruptedPath    = 103
	protoResultTypeSetPhase         = 104
	protoResultTypeProgress         = 105
	protoResultTypeSetExpected      = 106
	protoResultTypePostBuildLogLine = 107
	protoResultTypeFetchStatus      = 108
)

type ActionType int

const (
	ActionTypeStart ActionType = iota
	ActionTypeStop
	ActionTypeResult
	ActionTypeMessage
)

// Event is a common interface for all events.
type Event interface {
	Action() ActionType
}

// StartCopyPathsEvent
type StartCopyPathsEvent struct {
	ID     int64
	Parent int64
}

func (e StartCopyPathsEvent) Action() ActionType {
	return ActionTypeStart
}

// StartBuildsEvent
type StartBuildsEvent struct {
	ID     int64
	Parent int64
}

func (e StartBuildsEvent) Action() ActionType {
	return ActionTypeStart
}

// StartRealiseEvent
type StartRealiseEvent struct {
	ID     int64
	Parent int64
}

func (e StartRealiseEvent) Action() ActionType {
	return ActionTypeStart
}

// StartCopyPathEvent
type StartCopyPathEvent struct {
	ID   int64
	Path string
	From string
	To   string
	Text string
}

func (e StartCopyPathEvent) Action() ActionType {
	return ActionTypeStart
}

// StartBuildEvent
type StartBuildEvent struct {
	ID   int64
	Path string
	Text string
}

func (e StartBuildEvent) Action() ActionType {
	return ActionTypeStart
}

// StartFileTransferEvent
type StartFileTransferEvent struct {
	ID     int64
	Parent int64
	Path   string
	Text   string
}

func (e StartFileTransferEvent) Action() ActionType {
	return ActionTypeStart
}

// ResultProgressEvent
type ResultProgressEvent struct {
	ID       int64
	Done     int64
	Expected int64
	Running  int
	Failed   int
}

func (e ResultProgressEvent) Action() ActionType {
	return ActionTypeResult
}

// ResultSetPhaseEvent
type ResultSetPhaseEvent struct {
	ID    int64
	Phase string
}

func (e ResultSetPhaseEvent) Action() ActionType {
	return ActionTypeResult
}

// ResultSetExpectedCopyPathEvent
type ResultSetExpectedCopyPathEvent struct {
	ID       int64
	Expected int64
}

func (e ResultSetExpectedCopyPathEvent) Action() ActionType {
	return ActionTypeResult
}

// ResultSetExpectedFileTransferEvent
type ResultSetExpectedFileTransferEvent struct {
	ID       int64
	Expected int64
}

func (e ResultSetExpectedFileTransferEvent) Action() ActionType {
	return ActionTypeResult
}

// ResultBuildLogLineEvent
type ResultBuildLogLineEvent struct {
	ID   int64
	Text string
}

func (e ResultBuildLogLineEvent) Action() ActionType {
	return ActionTypeResult
}

// StopEvent
type StopEvent struct {
	ID int64
}

func (e StopEvent) Action() ActionType {
	return ActionTypeStop
}

// MessageEvent is an event containing a log message.
type MessageEvent struct {
	Text  string
	Level int
}

func (e MessageEvent) Action() ActionType {
	return ActionTypeMessage
}

type ProgressReporter interface {
	Run(context.Context, *ProgressDecoder) error
}

const protoPrefix = "@nix "

type ProgressDecoder struct {
	reader io.Reader
	prefix []byte
	plen   int
}

func NewProgressDecoder(r io.Reader) *ProgressDecoder {
	return &ProgressDecoder{
		reader: r,
		prefix: []byte(protoPrefix),
		plen:   len(protoPrefix),
	}
}

func (d *ProgressDecoder) Events(yield func(Event) bool) {
	scanner := bufio.NewScanner(d.reader)
	parser := &fastjson.Parser{}

	for scanner.Scan() {
		line := scanner.Bytes()

		// Just drop lines not starting with the "@nix " prefix
		if !bytes.HasPrefix(line, d.prefix) {
			continue
		}

		// Parse event
		val, err := parser.ParseBytes(line[d.plen:])
		if err != nil {
			continue
		}

		// Decode data
		if ev := decodeRawEvent(val); ev != nil {
			if !yield(ev) {
				return
			}
		}
	}
}

func decodeRawEvent(val *fastjson.Value) Event {
	action := val.GetStringBytes("action")
	// Ignore invalid event
	if action == nil {
		return nil
	}

	switch string(action) {
	case protoActionStart:
		return decodeRawStartEvent(val)

	case protoActionResult:
		return decodeRawResultEvent(val)

	case protoActionStop:
		id := val.GetInt64("id")
		// If ID is 0, we just ignore the event
		if id < 1 {
			return nil
		}
		return StopEvent{id}

	case protoActionMessage:
		lvl := val.GetInt("level")
		msg := val.GetStringBytes("msg")
		if msg == nil {
			return nil
		}
		return MessageEvent{string(msg), lvl}
	}

	return nil
}

func decodeRawStartEvent(val *fastjson.Value) Event {
	switch val.GetInt("type") {
	case protoEventTypeCopyPaths:
		return decodeRawStartCopyPathsEvent(val)
	case protoEventTypeBuilds:
		return decodeRawStartBuildsEvent(val)
	case protoEventTypeRealise:
		return decodeRawStartRealiseEvent(val)
	case protoEventTypeCopyPath:
		return decodeRawStartCopyPathEvent(val)
	case protoEventTypeBuild:
		return decodeRawStartBuildEvent(val)
	case protoEventTypeFileTransfer:
		return decodeRawStartFileTransferEvent(val)
	}

	return nil
}

func decodeRawStartCopyPathsEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	return StartCopyPathsEvent{
		ID:     id,
		Parent: val.GetInt64("parent"),
	}
}

func decodeRawStartBuildsEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	return StartBuildsEvent{
		ID:     id,
		Parent: val.GetInt64("parent"),
	}
}

func decodeRawStartRealiseEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	return StartRealiseEvent{
		ID:     id,
		Parent: val.GetInt64("parent"),
	}
}

func decodeRawStartCopyPathEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	// Parse fields
	fields := val.GetArray("fields")
	if fields == nil {
		return nil
	}
	if len(fields) < 3 {
		return nil
	}

	// Get path
	p := fields[0].GetStringBytes()
	if p == nil {
		return nil
	}

	// Get from
	from := fields[1].GetStringBytes()
	if from == nil {
		return nil
	}

	// Get to
	to := fields[2].GetStringBytes()
	if to == nil {
		return nil
	}

	// Get text
	text := val.GetStringBytes("text")
	if text == nil {
		text = []byte{}
	}

	return StartCopyPathEvent{
		ID:   id,
		Path: string(p),
		From: string(from),
		To:   string(to),
		Text: string(text),
	}
}

func decodeRawStartBuildEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	// Parse fields
	fields := val.GetArray("fields")
	if fields == nil {
		return nil
	}
	if len(fields) < 1 {
		return nil
	}

	// Get path
	p := fields[0].GetStringBytes()
	if p == nil {
		return nil
	}

	// Get text
	text := val.GetStringBytes("text")
	if text == nil {
		text = []byte{}
	}

	return StartBuildEvent{
		ID:   id,
		Path: string(p),
		Text: string(text),
	}
}

func decodeRawStartFileTransferEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	pid := val.GetInt64("parent")

	// Parse fields
	fields := val.GetArray("fields")
	if fields == nil {
		return nil
	}
	if len(fields) < 1 {
		return nil
	}

	// Get path
	p := fields[0].GetStringBytes()
	if p == nil {
		return nil
	}

	// Get text
	text := val.GetStringBytes("text")
	if text == nil {
		text = []byte{}
	}

	return StartFileTransferEvent{
		ID:     id,
		Parent: pid,
		Path:   string(p),
		Text:   string(text),
	}
}

func decodeRawResultEvent(val *fastjson.Value) Event {
	switch val.GetInt("type") {
	case protoResultTypeProgress:
		return decodeRawResultProgressEvent(val)
	case protoResultTypeSetExpected:
		return decodeRawResultSetExpectedEvent(val)
	case protoResultTypeSetPhase:
		return decodeRawResultSetPhaseEvent(val)
	case protoResultTypeBuildLogLine:
		return decodeRawResultBuildLogLineEvent(val)
	}

	return nil
}

func decodeRawResultProgressEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	// Parse fields
	fields := val.GetArray("fields")
	if fields == nil {
		return nil
	}
	if len(fields) < 4 {
		return nil
	}

	// Get fields
	done := fields[0].GetInt64()
	expected := fields[1].GetInt64()
	running := fields[2].GetInt()
	failed := fields[3].GetInt()

	return ResultProgressEvent{id, done, expected, running, failed}
}

func decodeRawResultSetPhaseEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	// Parse fields
	fields := val.GetArray("fields")
	if fields == nil {
		return nil
	}
	if len(fields) < 1 {
		return nil
	}

	// Parse phase
	phase := fields[0].GetStringBytes()
	if phase == nil {
		return nil
	}

	return ResultSetPhaseEvent{id, string(phase)}
}

func decodeRawResultSetExpectedEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	// Parse fields
	fields := val.GetArray("fields")
	if fields == nil {
		return nil
	}
	if len(fields) < 2 {
		return nil
	}

	// Parse phase
	evType := fields[0].GetInt()
	expected := fields[1].GetInt64()

	switch evType {
	case protoEventTypeCopyPath:
		return ResultSetExpectedCopyPathEvent{id, expected}
	case protoEventTypeFileTransfer:
		return ResultSetExpectedFileTransferEvent{id, expected}
	}

	return nil
}

func decodeRawResultBuildLogLineEvent(val *fastjson.Value) Event {
	id := val.GetInt64("id")
	// If ID is 0, we just ignore the event
	if id < 1 {
		return nil
	}

	// Parse fields
	fields := val.GetArray("fields")
	if fields == nil {
		return nil
	}
	if len(fields) < 1 {
		return nil
	}

	// Parse text
	text := fields[0].GetStringBytes()
	if text == nil {
		return nil
	}

	return ResultBuildLogLineEvent{id, string(text)}
}
