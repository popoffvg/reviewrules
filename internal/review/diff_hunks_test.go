package review

import (
	"reflect"
	"testing"
)

func TestDiffHunksKeepOnlyHunksThatAddALine(t *testing.T) {
	patch := `diff --git a/kept.go b/kept.go
index 1..2 100644
--- a/kept.go
+++ b/kept.go
@@ -1,2 +1,3 @@ func main() {
 a
+b
 c
@@ -10,2 +11,1 @@
 x
-y
diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1 +0,0 @@
-gone
diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1 @@
+new
diff --git "a/caf\303\251.go" "b/caf\303\251.go"
--- "a/caf\303\251.go"
+++ "b/caf\303\251.go"
@@ -1 +1 @@
-old
+quoted
`

	got := diffHunks(patch)

	want := []DiffHunk{
		{File: "kept.go", Header: "@@ -1,2 +1,3 @@ func main() {", NewStart: 1, Body: " a\n+b\n c\n"},
		{File: "new.go", Header: "@@ -0,0 +1 @@", NewStart: 1, Body: "+new\n"},
		{File: "café.go", Header: "@@ -1 +1 @@", NewStart: 1, Body: "-old\n+quoted\n"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diffHunks() = %#v, want %#v", got, want)
	}
}
