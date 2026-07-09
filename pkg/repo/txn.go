package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

const (
	TransactionStatusStarted     = "started"
	TransactionStatusPrepared    = "prepared"
	TransactionStatusCommitted   = "committed"
	TransactionStatusRolledBack  = "rolled_back"
	TransactionStatusNeedsRepair = "needs_repair"
)

type TransactionRecord struct {
	ID           string                   `json:"id"`
	Operation    string                   `json:"operation"`
	Status       string                   `json:"status"`
	StartedAt    string                   `json:"started_at"`
	UpdatedAt    string                   `json:"updated_at"`
	TouchedRefs  []TransactionRefMutation `json:"touched_refs,omitempty"`
	TouchedFiles []string                 `json:"touched_files,omitempty"`
	Error        string                   `json:"error,omitempty"`
}

type TransactionRefMutation struct {
	Ref     string      `json:"ref"`
	OldHash object.Hash `json:"old_hash,omitempty"`
	NewHash object.Hash `json:"new_hash,omitempty"`
}

type Transaction struct {
	repo *Repo
	path string
	rec  TransactionRecord
}

func (r *Repo) BeginTransaction(operation string) (*Transaction, error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "unknown"
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("%s-%d-%d", operation, now.UnixNano(), os.Getpid())
	tx := &Transaction{
		repo: r,
		path: filepath.Join(r.GraftDir, "txn", id+".json"),
		rec: TransactionRecord{
			ID:        id,
			Operation: operation,
			Status:    TransactionStatusStarted,
			StartedAt: now.Format(time.RFC3339Nano),
			UpdatedAt: now.Format(time.RFC3339Nano),
		},
	}
	if err := tx.write(); err != nil {
		return nil, err
	}
	return tx, nil
}

func (tx *Transaction) ID() string {
	if tx == nil {
		return ""
	}
	return tx.rec.ID
}

func (tx *Transaction) AddRef(ref string, oldHash, newHash object.Hash) error {
	if tx == nil {
		return nil
	}
	tx.rec.TouchedRefs = append(tx.rec.TouchedRefs, TransactionRefMutation{
		Ref:     ref,
		OldHash: oldHash,
		NewHash: newHash,
	})
	return tx.write()
}

func (tx *Transaction) AddFiles(paths []string) error {
	if tx == nil || len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tx.rec.TouchedFiles)+len(paths))
	for _, path := range tx.rec.TouchedFiles {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		seen[path] = struct{}{}
	}
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		seen[path] = struct{}{}
	}
	tx.rec.TouchedFiles = tx.rec.TouchedFiles[:0]
	for path := range seen {
		tx.rec.TouchedFiles = append(tx.rec.TouchedFiles, path)
	}
	sort.Strings(tx.rec.TouchedFiles)
	return tx.write()
}

func (tx *Transaction) AddFile(path string) error {
	if tx == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	return tx.AddFiles([]string{path})
}

func (tx *Transaction) Prepare() error {
	return tx.setStatus(TransactionStatusPrepared, "")
}

func (tx *Transaction) Commit() error {
	return tx.setStatus(TransactionStatusCommitted, "")
}

func (tx *Transaction) MarkNeedsRepair(reason string) error {
	return tx.setStatus(TransactionStatusNeedsRepair, reason)
}

func (tx *Transaction) setStatus(status, reason string) error {
	if tx == nil {
		return nil
	}
	tx.rec.Status = status
	tx.rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if strings.TrimSpace(reason) != "" {
		tx.rec.Error = reason
	}
	return tx.write()
}

func (tx *Transaction) write() error {
	data, err := json.MarshalIndent(tx.rec, "", "  ")
	if err != nil {
		return fmt.Errorf("transaction marshal: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(tx.path, data, 0o644); err != nil {
		return fmt.Errorf("transaction write: %w", err)
	}
	return nil
}

func (r *Repo) ListTransactions() ([]TransactionRecord, error) {
	root := filepath.Join(r.GraftDir, "txn")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	var records []TransactionRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read transaction %s: %w", entry.Name(), err)
		}
		var rec TransactionRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("read transaction %s: unmarshal: %w", entry.Name(), err)
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt < records[j].StartedAt
	})
	return records, nil
}

func (r *Repo) ReadTransaction(id string) (*TransactionRecord, error) {
	path, err := r.transactionPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read transaction %q: %w", id, err)
	}
	var rec TransactionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("read transaction %q: unmarshal: %w", id, err)
	}
	return &rec, nil
}

func (r *Repo) MarkTransactionRolledBack(id, reason string) (*TransactionRecord, error) {
	return r.setTransactionStatus(id, TransactionStatusRolledBack, reason)
}

func (r *Repo) MarkTransactionCommitted(id, reason string) (*TransactionRecord, error) {
	return r.setTransactionStatus(id, TransactionStatusCommitted, reason)
}

func (r *Repo) setTransactionStatus(id, status, reason string) (*TransactionRecord, error) {
	path, err := r.transactionPath(id)
	if err != nil {
		return nil, err
	}
	rec, err := r.ReadTransaction(id)
	if err != nil {
		return nil, err
	}
	tx := &Transaction{repo: r, path: path, rec: *rec}
	if err := tx.setStatus(status, reason); err != nil {
		return nil, err
	}
	return &tx.rec, nil
}

func (r *Repo) transactionPath(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("transaction id is required")
	}
	if filepath.Base(id) != id || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return "", fmt.Errorf("invalid transaction id %q", id)
	}
	if filepath.Ext(id) == ".json" {
		id = strings.TrimSuffix(id, ".json")
	}
	return filepath.Join(r.GraftDir, "txn", id+".json"), nil
}

func transactionIncomplete(status string) bool {
	switch status {
	case TransactionStatusStarted, TransactionStatusPrepared, TransactionStatusNeedsRepair:
		return true
	default:
		return false
	}
}
