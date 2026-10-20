// v26.71: effect patterns match whole name segments. The substring rule read
// parent.matrix_world as the ent ORM (database), pos.lerp as the os package
// (files) and magnet.pull as the network.
package flow

import "testing"

func TestEffectPatternsMatchWholeSegments(t *testing.T) {
	for _, tc := range []struct {
		callee   string
		patterns []string
		want     bool
	}{
		{"parent.matrix_world.inverted", dbPatterns, false},
		{"current.copy", dbPatterns, false},
		{"pos.lerp", fsPatterns, false},
		{"magnet.pull", netPatterns, false},
		{"db.Query", dbPatterns, true},
		{"s.db.Exec", dbPatterns, true},
		{"tx.ExecContext", dbWritePatterns, true},
		{"ent.Client", dbPatterns, true},
		{"os.Open", fsPatterns, true},
		{"ioutil.ReadFile", fsPatterns, true},
		{"http.Get", netPatterns, true},
		{"client.Do", netPatterns, true},
	} {
		if got := matchesAny(tc.callee, tc.patterns); got != tc.want {
			t.Errorf("matchesAny(%q) = %v, want %v", tc.callee, got, tc.want)
		}
	}
}

// v26.71: environment, process and path-string calls are no file I/O; calls
// that look at the disk still are.
func TestEnvironmentAndPathStringCallsAreNoFileIO(t *testing.T) {
	isFile := func(c string) bool { return (matchesAny(c, fsPatterns) && !notFileCall(c)) || pyFileCall(c) }
	for c, want := range map[string]bool{
		"os.environ.get": false, "os.getenv": false, "os.Getenv": false, "os.LookupEnv": false,
		"os.Exit": false, "os.cpu_count": false, "os.path.join": false, "os.path.basename": false,
		"filepath.Join": false, "filepath.Ext": false,
		"os.path.exists": true, "os.makedirs": true, "os.Open": true, "os.WriteFile": true,
		"filepath.Walk": true, "filepath.Glob": true, "ioutil.ReadFile": true,
		"open": true, "shutil.copytree": true, "p.write_text": true, "path.read_bytes": true,
		"d.mkdir": true, "open_dir": false, "self.open_dir": false, "mkdir": false,
	} {
		if got := isFile(c); got != want {
			t.Errorf("%s: file I/O %v, want %v", c, got, want)
		}
	}
}

// v26.74: a bare verb counts only as the method name on a receiver that names its kind.
func TestEffectVerbsNeedAReceiverHint(t *testing.T) {
	for _, tc := range []struct {
		callee   string
		patterns []string
		want     bool
	}{
		{"cache.Get", netPatterns, false}, {"GetUser", netPatterns, false}, {"s.store.GetByID", netPatterns, false},
		{"doc.Done", netPatterns, false}, {"list.Remove", fsPatterns, false}, {"b.Create", fsPatterns, false},
		{"CreateOrder", fsPatterns, false}, {"svc.OpenAccount", fsPatterns, false}, {"q.Query", dbPatterns, false},
		{"feedback.Exec", dbPatterns, false}, {"pos.Remove", fsPatterns, false}, {"rows.Get", netPatterns, false},
		{"s.httpClient.Do", netPatterns, true}, {"client.Get", netPatterns, true}, {"HTTPClient.Post", netPatterns, true},
		{"conn.Dial", netPatterns, true}, {"s.dbPool.QueryRowContext", dbPatterns, true}, {"tx.Exec", dbPatterns, true},
		{"sqlDB.Prepare", dbPatterns, true}, {"fs.Remove", fsPatterns, true}, {"file_store.Open", fsPatterns, true},
		{"afero.WriteFile", fsPatterns, true}, {"os.Create", fsPatterns, true}, {"http.Get", netPatterns, true},
	} {
		if got := matchesAny(tc.callee, tc.patterns); got != tc.want {
			t.Errorf("matchesAny(%q) = %v, want %v", tc.callee, got, tc.want)
		}
	}
}
