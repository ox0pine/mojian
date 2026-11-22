package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUntrackedDiffTrailingNewline(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "no trailing newline",
			data: "first\nlast",
			want: "diff --git a/note.txt b/note.txt\n" +
				"new file mode 100644\n" +
				"--- /dev/null\n" +
				"+++ b/note.txt\n" +
				"@@ -0,0 +1,2 @@\n" +
				"+first\n" +
				"+last\n" +
				"\\ No newline at end of file\n",
		},
		{
			name: "trailing newline",
			data: "first\nlast\n",
			want: "diff --git a/note.txt b/note.txt\n" +
				"new file mode 100644\n" +
				"--- /dev/null\n" +
				"+++ b/note.txt\n" +
				"@@ -0,0 +1,2 @@\n" +
				"+first\n" +
				"+last\n",
		},
		{
			name: "empty file",
			want: "Empty file.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte(tt.data), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := untrackedDiff(LocalRunner{}, dir, "note.txt", "note.txt")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("diff = %q, want %q", got, tt.want)
			}
		})
	}
}
