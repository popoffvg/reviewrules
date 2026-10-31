package review

import "testing"

func TestDecodeForkJSONReply(t *testing.T) {
	cases := []struct {
		name        string
		reply       string
		wantVerdict RuleVerdict
		wantCount   int
	}{
		{
			"unfenced-object-with-nested-findings",
			`{"verdict":"FAIL","findings":[{"file":"a.go","line":96,"message":"args"},{"file":"a.go","line":87,"message":"comment"}]}`,
			RuleVerdictFail,
			2,
		},
		{
			"fenced-object-after-prose",
			"I checked the rule.\n```json\n{\"verdict\": \"PASS\", \"findings\": []}\n```",
			RuleVerdictPass,
			0,
		},
		{
			"prose-braces-before-object",
			"The map {a: b} is fine.\n{\"verdict\":\"FAIL\",\"findings\":[{\"file\":\"b.go\",\"line\":1,\"message\":\"x\"}]}",
			RuleVerdictFail,
			1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reply ruleForkReply
			err := decodeForkJSONReply(tc.reply, "verdict", &reply)
			if err != nil {
				t.Fatalf("decodeForkJSONReply error: %v", err)
			}
			if reply.Verdict != tc.wantVerdict || len(reply.Findings) != tc.wantCount {
				t.Fatalf("got verdict %q with %d findings, want %q with %d", reply.Verdict, len(reply.Findings), tc.wantVerdict, tc.wantCount)
			}
		})
	}

	t.Run("no-object-with-key", func(t *testing.T) {
		var reply ruleForkReply
		err := decodeForkJSONReply(`{"file":"a.go","line":1}`, "verdict", &reply)
		if err == nil {
			t.Fatal("decodeForkJSONReply accepted an object without the verdict key")
		}
	})
}
