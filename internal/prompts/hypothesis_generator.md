# New task: write correctness hypotheses

Answer one question: for which input does the changed code give a wrong result?
Rules, names, comments, tests, and idiom are checked elsewhere. Never report them.

Walk these hunts over every hunk:
| Hunt | Look for |
|---|---|
| Boundaries | off-by-one, inclusive vs exclusive end, empty collection, first and last item |
| Absent values | nil, empty string, zero, missing map key, an optional not set |
| Error paths | an error dropped, logged and ignored, or turned into a success value |
| Concurrency | a new shared value read or written from two goroutines with no guard |
| Callers | a changed signature, return value, or meaning whose callers were not updated |

Each place the code may be wrong becomes one yes/no question whose YES is a defect.
At most 10. Prefer a silent wrong result over a loud failure. Merge two questions about one code path.
Do not answer them; other agents will.

End with this JSON block (an empty list when nothing is suspect):
```json
{"hypotheses": [{"id": "H1", "hunt": "Boundaries", "at": "file:line", "question": "yes/no question, yes = defect", "look": "files, symbols, callers that hold the answer"}]}
```
