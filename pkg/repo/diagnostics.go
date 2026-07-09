package repo

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

const RepositoryDiagnosticsSchemaVersion = 1

type DiagnosticSeverity string

const (
	DiagnosticInfo    DiagnosticSeverity = "info"
	DiagnosticWarning DiagnosticSeverity = "warning"
	DiagnosticError   DiagnosticSeverity = "error"
)

type RepositoryDiagnostic struct {
	Severity  DiagnosticSeverity `json:"severity"`
	Code      string             `json:"code"`
	Message   string             `json:"message"`
	Path      string             `json:"path,omitempty"`
	Ref       string             `json:"ref,omitempty"`
	Object    string             `json:"object,omitempty"`
	Repair    string             `json:"repair,omitempty"`
	Operation string             `json:"operation,omitempty"`
}

type RepositoryIntegrityReport struct {
	SchemaVersion int                    `json:"schema_version"`
	OK            bool                   `json:"ok"`
	LooseObjects  int                    `json:"loose_objects"`
	PackFiles     int                    `json:"pack_files"`
	PackObjects   int                    `json:"pack_objects"`
	Diagnostics   []RepositoryDiagnostic `json:"diagnostics,omitempty"`
}

func (r *Repo) VerifyIntegrity() *RepositoryIntegrityReport {
	report := &RepositoryIntegrityReport{
		SchemaVersion: RepositoryDiagnosticsSchemaVersion,
		OK:            true,
	}

	if storeReport, err := r.Store.Verify(); err != nil {
		report.add(RepositoryDiagnostic{
			Severity: DiagnosticError,
			Code:     "object_store_invalid",
			Message:  err.Error(),
			Repair:   "restore missing/corrupt objects from a trusted remote or backup",
		})
	} else if storeReport != nil {
		report.LooseObjects = storeReport.LooseObjects
		report.PackFiles = storeReport.PackFiles
		report.PackObjects = storeReport.PackObjects
	}

	for _, d := range r.repositoryConfigDiagnostics() {
		report.add(d)
	}
	for _, d := range r.refDiagnostics() {
		report.add(d)
	}
	for _, d := range r.indexDiagnostics() {
		report.add(d)
	}
	for _, d := range r.reflogDiagnostics() {
		report.add(d)
	}
	for _, d := range r.transactionDiagnostics() {
		report.add(d)
	}
	for _, d := range r.repositoryLockDiagnostics() {
		report.add(d)
	}
	for _, d := range r.gitShadowDiagnostics() {
		report.add(d)
	}
	for _, d := range r.coordFeedDiagnostics() {
		report.add(d)
	}

	sort.SliceStable(report.Diagnostics, func(i, j int) bool {
		si, sj := severityRank(report.Diagnostics[i].Severity), severityRank(report.Diagnostics[j].Severity)
		if si != sj {
			return si > sj
		}
		if report.Diagnostics[i].Code != report.Diagnostics[j].Code {
			return report.Diagnostics[i].Code < report.Diagnostics[j].Code
		}
		return report.Diagnostics[i].Message < report.Diagnostics[j].Message
	})
	return report
}

func (r *RepositoryIntegrityReport) add(d RepositoryDiagnostic) {
	r.Diagnostics = append(r.Diagnostics, d)
	if d.Severity == DiagnosticError {
		r.OK = false
	}
}

func severityRank(s DiagnosticSeverity) int {
	switch s {
	case DiagnosticError:
		return 3
	case DiagnosticWarning:
		return 2
	default:
		return 1
	}
}

func (r *Repo) repositoryConfigDiagnostics() []RepositoryDiagnostic {
	cfgPath := r.repositoryConfigPath()
	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		return []RepositoryDiagnostic{{
			Severity: DiagnosticError,
			Code:     "repository_config_invalid",
			Message:  err.Error(),
			Path:     cfgPath,
			Repair:   "repair or recreate .graft/config from repository metadata",
		}}
	}
	var out []RepositoryDiagnostic
	if cfg.RepositoryFormatVersion == 0 {
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticWarning,
			Code:     "repository_config_missing",
			Message:  ".graft/config is missing; treating repository as legacy format",
			Path:     cfgPath,
			Repair:   "run graft repair migrate-config when available",
		})
		return out
	}
	if cfg.RepositoryFormatVersion > RepositoryFormatVersion {
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticError,
			Code:     "repository_format_unsupported",
			Message:  fmt.Sprintf("repository format %d is newer than supported format %d", cfg.RepositoryFormatVersion, RepositoryFormatVersion),
			Path:     cfgPath,
			Repair:   "upgrade graft before using this repository",
		})
	}
	if cfg.ObjectHash != "" && cfg.ObjectHash != DefaultRepositoryObjectHash {
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticError,
			Code:     "repository_hash_unsupported",
			Message:  fmt.Sprintf("object_hash %q is not supported", cfg.ObjectHash),
			Path:     cfgPath,
			Repair:   "open with a graft version that supports this object hash",
		})
	}
	return out
}

func (r *Repo) refDiagnostics() []RepositoryDiagnostic {
	refs, err := r.ListRefs("")
	if err != nil {
		return []RepositoryDiagnostic{{
			Severity: DiagnosticError,
			Code:     "refs_unreadable",
			Message:  err.Error(),
			Path:     filepath.Join(r.refsBaseDir(), "refs"),
			Repair:   "inspect refs under .graft/refs and restore malformed refs",
		}}
	}

	var out []RepositoryDiagnostic
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h := refs[name]
		refName := "refs/" + name
		if strings.HasPrefix(name, "heads/") {
			if _, err := r.Store.ReadCommit(h); err != nil {
				out = append(out, RepositoryDiagnostic{
					Severity: DiagnosticError,
					Code:     "ref_target_unreachable",
					Message:  fmt.Sprintf("branch ref %s points at unreadable commit %s: %v", refName, h, err),
					Ref:      refName,
					Object:   string(h),
					Repair:   "restore the referenced commit object or move the ref to a reachable commit",
				})
			}
			continue
		}
		if _, _, err := r.Store.Read(h); err != nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "ref_target_unreachable",
				Message:  fmt.Sprintf("ref %s points at unreadable object %s: %v", refName, h, err),
				Ref:      refName,
				Object:   string(h),
				Repair:   "restore the referenced object or remove the stale ref",
			})
		}
	}
	if head, err := r.Head(); err == nil && !strings.HasPrefix(head, "refs/") {
		if err := object.ValidateHash(head); err != nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "head_invalid",
				Message:  fmt.Sprintf("detached HEAD is not a valid object hash: %v", err),
				Ref:      "HEAD",
				Repair:   "reattach HEAD to a branch or restore a valid commit hash",
			})
		} else if _, err := r.Store.ReadCommit(object.Hash(head)); err != nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "head_unreachable",
				Message:  fmt.Sprintf("detached HEAD points at unreadable commit %s: %v", head, err),
				Ref:      "HEAD",
				Object:   head,
				Repair:   "restore the commit or reattach HEAD to a valid branch",
			})
		}
	}
	return out
}

func (r *Repo) indexDiagnostics() []RepositoryDiagnostic {
	stg, err := r.ReadStaging()
	if err != nil {
		return []RepositoryDiagnostic{{
			Severity: DiagnosticError,
			Code:     "index_unreadable",
			Message:  err.Error(),
			Path:     r.indexPath(),
			Repair:   "restore .graft/index from backup or rebuild staging state",
		}}
	}
	var out []RepositoryDiagnostic
	paths := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		entry := stg.Entries[p]
		if entry == nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "index_entry_nil",
				Message:  fmt.Sprintf("index entry %s is empty", p),
				Path:     p,
				Repair:   "remove or restage the path",
			})
			continue
		}
		for _, d := range entry.ExtractionDiagnostics {
			out = append(out, RepositoryDiagnostic{
				Severity: diagnosticSeverityFromString(d.Severity),
				Code:     d.Code,
				Message:  d.Message,
				Path:     p,
				Repair:   extractionDiagnosticRepair(d.Code),
			})
		}
		if entry.Mode != object.TreeModeModule && entry.BlobHash != "" {
			if _, err := r.Store.ReadBlob(entry.BlobHash); err != nil {
				out = append(out, RepositoryDiagnostic{
					Severity: DiagnosticError,
					Code:     "index_blob_unreachable",
					Message:  fmt.Sprintf("index entry %s points at unreadable blob %s: %v", p, entry.BlobHash, err),
					Path:     p,
					Object:   string(entry.BlobHash),
					Repair:   "restage the path or restore the blob object",
				})
			}
		}
		if entry.EntityListHash != "" {
			el, err := r.Store.ReadEntityList(entry.EntityListHash)
			if err != nil {
				out = append(out, RepositoryDiagnostic{
					Severity: DiagnosticError,
					Code:     "index_entity_list_unreachable",
					Message:  fmt.Sprintf("index entry %s points at unreadable entity list %s: %v", p, entry.EntityListHash, err),
					Path:     p,
					Object:   string(entry.EntityListHash),
					Repair:   "restage the path or restore the entity-list object",
				})
				continue
			}
			for _, entityHash := range el.EntityRefs {
				if _, err := r.Store.ReadEntity(entityHash); err != nil {
					out = append(out, RepositoryDiagnostic{
						Severity: DiagnosticError,
						Code:     "index_entity_unreachable",
						Message:  fmt.Sprintf("index entry %s references unreadable entity %s: %v", p, entityHash, err),
						Path:     p,
						Object:   string(entityHash),
						Repair:   "restage the path or restore the entity object",
					})
				}
			}
		}
	}
	return out
}

func diagnosticSeverityFromString(severity string) DiagnosticSeverity {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "error":
		return DiagnosticError
	case "warning", "warn":
		return DiagnosticWarning
	default:
		return DiagnosticInfo
	}
}

func extractionDiagnosticRepair(code string) string {
	switch code {
	case "entity_extraction_parse_errors":
		return "fix source syntax and rerun graft add for this path"
	case "entity_extraction_oversized":
		return "reduce file size, add it to .graftignore, or accept blob-only tracking"
	case "entity_extraction_data_format_skipped":
		return "rerun graft add with entity forcing when structural data-file tracking is required"
	case "entity_extraction_failed":
		return "inspect parser support for this language and rerun graft add after fixing the source or upgrading graft"
	default:
		return ""
	}
}

func (r *Repo) reflogDiagnostics() []RepositoryDiagnostic {
	var out []RepositoryDiagnostic
	root := filepath.Join(r.refsBaseDir(), "logs")
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return []RepositoryDiagnostic{{
			Severity: DiagnosticError,
			Code:     "reflog_root_unreadable",
			Message:  err.Error(),
			Path:     root,
			Repair:   "repair .graft/logs permissions or restore reflog metadata",
		}}
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "reflog_unreadable",
				Message:  walkErr.Error(),
				Path:     path,
				Repair:   "repair or remove unreadable reflog path",
			})
			return nil
		}
		if d.IsDir() {
			return nil
		}
		out = append(out, validateReflogFile(path)...)
		return nil
	})
	if err != nil {
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticError,
			Code:     "reflog_walk_failed",
			Message:  err.Error(),
			Path:     root,
			Repair:   "repair .graft/logs permissions",
		})
	}
	return out
}

func (r *Repo) transactionDiagnostics() []RepositoryDiagnostic {
	records, err := r.ListTransactions()
	if err != nil {
		return []RepositoryDiagnostic{{
			Severity: DiagnosticError,
			Code:     "transaction_records_unreadable",
			Message:  err.Error(),
			Path:     filepath.Join(r.GraftDir, "txn"),
			Repair:   "inspect .graft/txn and remove or repair malformed transaction records",
		}}
	}
	var out []RepositoryDiagnostic
	for _, rec := range records {
		if !transactionIncomplete(rec.Status) {
			continue
		}
		message := fmt.Sprintf("transaction %s for %s is %s", rec.ID, rec.Operation, rec.Status)
		if rec.Error != "" {
			message += ": " + rec.Error
		}
		out = append(out, RepositoryDiagnostic{
			Severity:  DiagnosticError,
			Code:      "transaction_incomplete",
			Message:   message,
			Path:      filepath.Join(r.GraftDir, "txn", rec.ID+".json"),
			Repair:    fmt.Sprintf("inspect the transaction record, then run graft repair transaction %s --mark-rolled-back --reason <note> after confirming refs and worktree state", rec.ID),
			Operation: rec.Operation,
		})
	}
	return out
}

func (r *Repo) repositoryLockDiagnostics() []RepositoryDiagnostic {
	status, err := r.RepositoryLockStatus()
	if err != nil {
		return []RepositoryDiagnostic{{
			Severity: DiagnosticError,
			Code:     "repository_lock_unreadable",
			Message:  err.Error(),
			Path:     r.repositoryLockPath(),
			Repair:   "repair .graft/locks permissions or remove stale lock metadata after confirming no graft operation is running",
		}}
	}
	if !status.Exists {
		return nil
	}
	if status.Stale {
		message := "repository lock is stale"
		if status.Reason != "" {
			message += ": " + status.Reason
		}
		return []RepositoryDiagnostic{{
			Severity:  DiagnosticError,
			Code:      "repository_lock_stale",
			Message:   message,
			Path:      status.Path,
			Repair:    "run graft repair clear-stale-locks",
			Operation: status.Info.Operation,
		}}
	}
	message := fmt.Sprintf(
		"repository lock held by %s pid %d on %s since %s",
		status.Info.Operation,
		status.Info.PID,
		status.Info.Hostname,
		status.Info.StartedAt,
	)
	return []RepositoryDiagnostic{{
		Severity:  DiagnosticInfo,
		Code:      "repository_lock_active",
		Message:   message,
		Path:      status.Path,
		Operation: status.Info.Operation,
	}}
}

func validateReflogFile(path string) []RepositoryDiagnostic {
	f, err := os.Open(path)
	if err != nil {
		return []RepositoryDiagnostic{{
			Severity: DiagnosticError,
			Code:     "reflog_unreadable",
			Message:  err.Error(),
			Path:     path,
			Repair:   "restore or remove the unreadable reflog",
		}}
	}
	defer f.Close()

	var out []RepositoryDiagnostic
	var lastNew object.Hash
	lineNo := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "reflog_line_malformed",
				Message:  fmt.Sprintf("reflog line %d has %d field(s), expected at least 4", lineNo, len(fields)),
				Path:     path,
				Repair:   "remove or repair the malformed reflog line",
			})
			continue
		}
		oldHash := object.Hash(fields[0])
		newHash := object.Hash(fields[1])
		if oldHash != zeroHash {
			if err := object.ValidateHash(string(oldHash)); err != nil {
				out = append(out, RepositoryDiagnostic{
					Severity: DiagnosticError,
					Code:     "reflog_old_hash_invalid",
					Message:  fmt.Sprintf("reflog line %d has invalid old hash: %v", lineNo, err),
					Path:     path,
					Repair:   "repair or remove the malformed reflog line",
				})
			}
		}
		if newHash != zeroHash {
			if err := object.ValidateHash(string(newHash)); err != nil {
				out = append(out, RepositoryDiagnostic{
					Severity: DiagnosticError,
					Code:     "reflog_new_hash_invalid",
					Message:  fmt.Sprintf("reflog line %d has invalid new hash: %v", lineNo, err),
					Path:     path,
					Repair:   "repair or remove the malformed reflog line",
				})
			}
		}
		if lineNo > 1 && oldHash != lastNew {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticWarning,
				Code:     "reflog_discontinuity",
				Message:  fmt.Sprintf("reflog line %d old hash %s does not match previous new hash %s", lineNo, oldHash, lastNew),
				Path:     path,
				Repair:   "inspect reflog history and regenerate it if necessary",
			})
		}
		lastNew = newHash
	}
	if err := scanner.Err(); err != nil {
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticError,
			Code:     "reflog_read_failed",
			Message:  err.Error(),
			Path:     path,
			Repair:   "repair or remove the unreadable reflog",
		})
	}
	return out
}

func (r *Repo) gitShadowDiagnostics() []RepositoryDiagnostic {
	var out []RepositoryDiagnostic
	if d, err := r.gitMapDiagnostics(); err != nil {
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticError,
			Code:     "git_shadow_map_unreadable",
			Message:  err.Error(),
			Path:     filepath.Join(r.GraftDir, gitMapFile),
			Repair:   "repair or remove malformed .graft/gitmap entries",
		})
	} else {
		out = append(out, d...)
	}

	status, err := r.GitShadowStatus()
	if err != nil {
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticWarning,
			Code:     "git_shadow_status_unreadable",
			Message:  status.Message + ": " + err.Error(),
			Repair:   "inspect the git shadow repository and run graft repair resync-git",
		})
		return out
	}
	switch status.State {
	case GitShadowStateFailureLog:
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticWarning,
			Code:     "git_shadow_failure_log_present",
			Message:  status.Message,
			Path:     filepath.Join(r.GraftDir, shadowFailuresLog),
			Repair:   "run graft repair resync-git, then graft repair clear-shadow-failures",
		})
	case GitShadowStateUnknownMapping:
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticWarning,
			Code:     "git_shadow_mapping_unknown",
			Message:  status.Message,
			Object:   string(status.GraftHead),
			Repair:   "run graft repair resync-git",
		})
	case GitShadowStateDivergedCommit, GitShadowStateDivergedTree:
		out = append(out, RepositoryDiagnostic{
			Severity: DiagnosticError,
			Code:     "git_shadow_" + status.State,
			Message:  status.Message,
			Object:   string(status.GraftHead),
			Repair:   "run graft repair resync-git",
		})
	}
	return out
}

func (r *Repo) gitMapDiagnostics() ([]RepositoryDiagnostic, error) {
	path := filepath.Join(r.GraftDir, gitMapFile)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []RepositoryDiagnostic
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 && len(fields) != 5 {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "git_shadow_map_line_malformed",
				Message:  fmt.Sprintf("gitmap line %d has %d field(s), expected 3 legacy fields or 5 current fields", lineNo, len(fields)),
				Path:     path,
				Repair:   "remove or repair malformed .graft/gitmap lines",
			})
			continue
		}
		if err := object.ValidateHash(fields[0]); err != nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "git_shadow_map_graft_hash_invalid",
				Message:  fmt.Sprintf("gitmap line %d has invalid graft hash: %v", lineNo, err),
				Path:     path,
				Repair:   "remove or repair malformed .graft/gitmap lines",
			})
		}
		if len(fields[1]) != 40 || len(fields[2]) != 40 {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "git_shadow_map_git_hash_invalid",
				Message:  fmt.Sprintf("gitmap line %d has invalid git commit/tree width", lineNo),
				Path:     path,
				Repair:   "run graft repair resync-git",
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

const (
	coordFeedHeadRef              = "refs/coord/feed/head"
	maxCoordFeedDiagnosticEntries = 100000
)

type coordFeedDiagnosticEntry struct {
	Parent string          `json:"parent,omitempty"`
	Event  json.RawMessage `json:"event"`
}

func (r *Repo) coordFeedDiagnostics() []RepositoryDiagnostic {
	refs, err := r.ListRefs("coord/feed")
	if err != nil {
		return []RepositoryDiagnostic{{
			Severity: DiagnosticError,
			Code:     "coord_feed_refs_unreadable",
			Message:  err.Error(),
			Path:     filepath.Join(r.refsBaseDir(), "refs", "coord", "feed"),
			Repair:   "inspect refs/coord/feed and remove or repair malformed coordination feed refs",
		}}
	}

	head, ok := refs["coord/feed/head"]
	if !ok || head == "" {
		return nil
	}

	var out []RepositoryDiagnostic
	seen := make(map[object.Hash]int)
	current := head
	for depth := 0; current != ""; depth++ {
		if depth >= maxCoordFeedDiagnosticEntries {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "coord_feed_chain_too_deep",
				Message:  fmt.Sprintf("coord feed chain exceeds %d entries", maxCoordFeedDiagnosticEntries),
				Ref:      coordFeedHeadRef,
				Object:   string(current),
				Repair:   "inspect refs/coord/feed/head and truncate the feed chain to the most recent valid entry",
			})
			return out
		}

		if firstDepth, exists := seen[current]; exists {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "coord_feed_cycle",
				Message:  fmt.Sprintf("coord feed cycle detected at entry %s first seen at depth %d", current, firstDepth),
				Ref:      coordFeedHeadRef,
				Object:   string(current),
				Repair:   "inspect refs/coord/feed/head and truncate the feed chain before the cycle",
			})
			return out
		}
		seen[current] = depth

		blob, err := r.Store.ReadBlob(current)
		if err != nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "coord_feed_entry_unreachable",
				Message:  fmt.Sprintf("coord feed entry %s is unreadable: %v", current, err),
				Ref:      coordFeedHeadRef,
				Object:   string(current),
				Repair:   "restore the missing feed blob from a trusted remote or truncate the feed chain to a reachable entry",
			})
			return out
		}

		var entry coordFeedDiagnosticEntry
		if err := json.Unmarshal(blob.Data, &entry); err != nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "coord_feed_entry_malformed",
				Message:  fmt.Sprintf("coord feed entry %s is not valid feed JSON: %v", current, err),
				Ref:      coordFeedHeadRef,
				Object:   string(current),
				Repair:   "inspect the feed blob and truncate refs/coord/feed/head to the most recent valid entry",
			})
			return out
		}
		if len(entry.Event) == 0 || string(entry.Event) == "null" {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "coord_feed_event_missing",
				Message:  fmt.Sprintf("coord feed entry %s does not contain an event", current),
				Ref:      coordFeedHeadRef,
				Object:   string(current),
				Repair:   "inspect the feed blob and truncate refs/coord/feed/head to the most recent valid entry",
			})
			return out
		}

		parent := strings.TrimSpace(entry.Parent)
		if parent == "" {
			return out
		}
		if err := object.ValidateHash(parent); err != nil {
			out = append(out, RepositoryDiagnostic{
				Severity: DiagnosticError,
				Code:     "coord_feed_parent_invalid",
				Message:  fmt.Sprintf("coord feed entry %s has invalid parent hash %q: %v", current, parent, err),
				Ref:      coordFeedHeadRef,
				Object:   string(current),
				Repair:   "inspect the feed blob and truncate refs/coord/feed/head to the most recent valid entry",
			})
			return out
		}
		current = object.Hash(parent)
	}
	return out
}
