package config

// SetRename replaces the rename that puts a finished file over sources.toml, and returns
// the undo.
func SetRename(f func(oldpath, newpath string) error) (restore func()) {
	old := rename
	rename = f
	return func() { rename = old }
}

// SetBuilt lets f change the new contents after they are built and before they are checked,
// and returns the undo.
func SetBuilt(f func([]byte) []byte) (restore func()) {
	buildHook = f
	return func() { buildHook = nil }
}

// Verify runs the pre-write check on contents the test supplies; op is add, replace or
// remove.
func Verify(path string, before, after []byte, op string, s Section) error {
	kinds := map[string]opKind{"add": opAdd, "replace": opReplace, "remove": opRemove}
	kind, ok := kinds[op]
	if !ok {
		panic("Verify: unknown op " + op)
	}
	return verify(path, before, after, kind, s, "")
}
