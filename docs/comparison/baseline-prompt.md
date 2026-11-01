Review the git diff below against ALL the repository rules below and also look for correctness defects not covered by a rule. Review only added or changed code. Read repository files when necessary. Use one review session; do not delegate, fork, or spawn other agents. Do not edit files, execute the code, build, or run tests. Follow the same rule coverage discipline for every rule: split atomic requirements, walk every applicable changed line, and record evidence for PASS, FAIL, or N/A. Report only actionable defects supported by the code and the stated contracts. Do not speculate about callers violating the stated input contracts.

End with one fenced JSON block with this shape:

```json
{"rules":[{"id":"rule id","verdict":"PASS or FAIL or N/A","evidence":"what was checked","findings":[{"file":"offers.go","line":1,"message":"problem"}]}],"correctness":[{"file":"offers.go","line":1,"message":"defect and a concrete input that demonstrates it"}]}
```

Append the contents of `.reviewrules/config.json`, then the output of `git diff --no-color main -- offers.go`.
