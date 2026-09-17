package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/codexadapter"
)

const validInput = `{"protocol":"metro.codex.action.v0.1","request_id":"req-1","action_id":"act-1","kind":"hash_text","text":"abc","side_effect":false}`

func TestCLIExampleRoundTrip(t *testing.T) {
	for _, name := range []string{"hash-text", "approval"} {
		data, err := os.ReadFile("../../examples/codex-" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var out, verified bytes.Buffer
		if err := execute([]string{"run"}, bytes.NewReader(data), &out); err != nil {
			t.Fatal(err)
		}
		if err := execute([]string{"verify"}, &out, &verified); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(verified.String(), `"verified": true`) {
			t.Fatal(verified.String())
		}
	}
}

func TestCLIRejectsAmbiguousOrUnboundedJSONWithoutProof(t *testing.T) {
	cases := []string{
		"", "null", "[]", validInput + "{}", validInput + "garbage",
		strings.Replace(validInput, `"side_effect":false`, `"side_effect":false,"side_effect":true`, 1),
		strings.Replace(validInput, `"side_effect":false`, `"side_effect":null`, 1),
		strings.Replace(validInput, `"side_effect":false`, `"side_effect":"false"`, 1),
		strings.Replace(validInput, `"side_effect":false`, `"Side_Effect":false`, 1),
		strings.Replace(validInput, `"side_effect":false`, `"side_effect":false,"approved":true`, 1),
		strings.Replace(validInput, `"side_effect":false`, `"side_effect":false,"allowed_targets":["shell"]`, 1),
		strings.Replace(validInput, `"side_effect":false`, `"side_effect":false,"state":{"x":"a","x":"b"}`, 1),
		strings.Replace(validInput, `"abc"`, `"\ud800"`, 1),
		strings.Replace(validInput, `"abc"`, `"\udc00"`, 1),
		strings.Replace(validInput, `"abc"`, `"\ud800\u0061"`, 1),
		strings.Replace(validInput, `"abc"`, "\""+string([]byte{0xff})+"\"", 1),
		strings.Repeat(" ", codexadapter.MaxInputBytes) + validInput,
		`{"state":` + strings.Repeat("[", 34) + `0` + strings.Repeat("]", 34) + `}`,
	}
	for _, input := range cases {
		var out bytes.Buffer
		if err := execute([]string{"run"}, strings.NewReader(input), &out); err == nil || out.Len() != 0 {
			t.Fatalf("invalid input produced proof: %q, %v", out.String(), err)
		}
	}
}

func TestCLIUnicodeAndInertShellText(t *testing.T) {
	for _, text := range []string{`\ud83d\ude80`, `Привет 🚇`, `$(touch /tmp/never); & whoami`, `\\ud800`, ``} {
		var out bytes.Buffer
		input := strings.Replace(validInput, "abc", text, 1)
		if err := execute([]string{"run"}, strings.NewReader(input), &out); err != nil {
			t.Fatal(err)
		}
		var response codexadapter.Response
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if err := codexadapter.Verify(response); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCLIUsageAndForgedProof(t *testing.T) {
	for _, args := range [][]string{nil, {"run", "--approve"}, {"execute"}, {"verify"}} {
		var out bytes.Buffer
		if err := execute(args, strings.NewReader(validInput), &out); err == nil || out.Len() > 0 {
			t.Fatal("invalid invocation accepted")
		}
	}
}

func FuzzCLIInput(f *testing.F) {
	f.Add(validInput)
	f.Add(`{"text":"\ud800"}`)
	f.Add("null")
	f.Fuzz(func(t *testing.T, input string) {
		var out bytes.Buffer
		err := execute([]string{"run"}, strings.NewReader(input), &out)
		if err != nil {
			if out.Len() != 0 {
				t.Fatal("failure emitted output")
			}
			return
		}
		var verified bytes.Buffer
		if err := execute([]string{"verify"}, &out, &verified); err != nil {
			t.Fatalf("emitted unverifiable response: %v", err)
		}
	})
}
