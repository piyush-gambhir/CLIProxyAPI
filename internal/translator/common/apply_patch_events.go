package common

import "github.com/MoeForge/moego/ojson"

type ApplyPatchCallState struct {
	ItemID      string
	CallID      string
	Name        string
	Namespace   string
	OutputIndex int
	Decoder     ApplyPatchInputDecoder
}

func (s *ApplyPatchCallState) PushArguments(fragment string) (string, error) {
	return s.Decoder.Push(fragment)
}

func (s *ApplyPatchCallState) FinishArguments(arguments string) (string, string, error) {
	tail, errFinish := s.Decoder.Finish(arguments)
	if errFinish != nil {
		return "", "", errFinish
	}
	return tail, s.Decoder.Input(), nil
}

func ApplyPatchInputDelta(s *ApplyPatchCallState, delta string, sequence int) []byte {
	doc := ojson.NewDoc([]byte(`{"type":"response.custom_tool_call_input.delta","item_id":"","call_id":"","output_index":0,"sequence_number":0,"delta":""}`))
	applyPatchEventIdentityDoc(doc, s, sequence)
	_ = doc.Set("delta", delta)
	return doc.Bytes()
}

func ApplyPatchInputDone(s *ApplyPatchCallState, input string, sequence int) []byte {
	doc := ojson.NewDoc([]byte(`{"type":"response.custom_tool_call_input.done","item_id":"","call_id":"","output_index":0,"sequence_number":0,"input":""}`))
	applyPatchEventIdentityDoc(doc, s, sequence)
	_ = doc.Set("input", input)
	return doc.Bytes()
}

func applyPatchEventIdentityDoc(doc *ojson.Doc, s *ApplyPatchCallState, sequence int) {
	_ = doc.Set("item_id", s.ItemID)
	_ = doc.Set("call_id", s.CallID)
	_ = doc.Set("output_index", s.OutputIndex)
	_ = doc.Set("sequence_number", sequence)
}

func ApplyPatchFailure(responseID string, sequence int) []byte {
	doc := ojson.NewDoc([]byte(`{"type":"response.failed","sequence_number":0,"response":{"id":"","object":"response","status":"failed","error":{"type":"server_error","code":"invalid_tool_arguments","message":"Invalid apply_patch tool arguments received from upstream.","param":null}}}`))
	_ = doc.Set("response.id", responseID)
	_ = doc.Set("sequence_number", sequence)
	return doc.Bytes()
}

type ApplyPatchErrorState struct {
	err error
}

func (s *ApplyPatchErrorState) SetToolInputError(err error) {
	s.err = err
}

func (s *ApplyPatchErrorState) ToolInputError() error {
	return s.err
}
