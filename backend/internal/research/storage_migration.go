package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type StorageMigrationReport struct {
	Missing   []string `json:"missing_objects,omitempty"`
	Recovered int      `json:"recovered_exact_files"`
	Records   int      `json:"saved_run_records"`
	Files     int      `json:"files"`
	Documents int      `json:"documents"`
	Bytes     int64    `json:"bytes"`
	Removed   int      `json:"local_files_removed"`
}

func verifyStoredObject(ctx context.Context, objects ObjectStorage, key string, expected []byte) error {
	body, _, err := objects.Get(ctx, key)
	if err != nil {
		return err
	}
	defer body.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(body, int64(len(expected))+1))
	if err != nil {
		return err
	}
	want := sha256.Sum256(expected)
	if n != int64(len(expected)) || !bytes.Equal(hash.Sum(nil), want[:]) {
		return fmt.Errorf("object verification failed: %s", key)
	}
	return nil
}
func copyVerifiedObject(ctx context.Context, objects ObjectStorage, key, contentType string, raw []byte) error {
	body, _, err := objects.Get(ctx, key)
	if err == nil {
		body.Close()
		return verifyStoredObject(ctx, objects, key, raw)
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if err = objects.Put(ctx, key, contentType, raw); err != nil {
		return err
	}
	return verifyStoredObject(ctx, objects, key, raw)
}
func (s *Store) clearMigratedDocuments(docs []ResearchDocument) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, d := range docs {
		result, err := tx.Exec("UPDATE research_documents SET content='',evidence=$1 WHERE id=$2 AND content=$3 AND evidence=$4", []byte{}, d.ID, d.Content, []byte(d.Evidence))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("document changed during migration; retry with the API stopped")
		}
	}
	return tx.Commit()
}

// MigrateArtifacts never traverses caches or arbitrary project files. Only
// database-referenced research files are copied. Stop the API during cutover.
func MigrateArtifacts(ctx context.Context, store *Store, root string, target ObjectStorage, removeLocal bool) (StorageMigrationReport, error) {
	return migrateArtifacts(ctx, store, root, target, removeLocal, false)
}
func MigrateArtifactsAllowMissing(ctx context.Context, store *Store, root string, target ObjectStorage, removeLocal bool) (StorageMigrationReport, error) {
	return migrateArtifacts(ctx, store, root, target, removeLocal, true)
}
func migrateArtifacts(ctx context.Context, store *Store, root string, target ObjectStorage, removeLocal, allowMissing bool) (StorageMigrationReport, error) {
	var report StorageMigrationReport
	for _, status := range []string{"queued", "running", "planning"} {
		_, n, err := store.List(status, "", "", 1, 0)
		if err != nil {
			return report, err
		}
		if n > 0 {
			return report, errors.New("finish or cancel active research jobs, then stop the API before migrating")
		}
	}
	local := LocalStorage{Root: root}
	remove := map[string]bool{}
	transfer := func(key, contentType, expectedHash string, expectedSize int64, candidate []byte) error {
		filename, err := local.filename(key)
		if err != nil {
			return err
		}
		real, resolveErr := filepath.EvalSymlinks(filename)
		if resolveErr == nil {
			base, err := filepath.EvalSymlinks(root)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(base, real)
			if err != nil || !validObjectKey(filepath.ToSlash(rel)) {
				return errors.New("local artifact resolves outside DATA_DIR")
			}
		}
		raw, err := readObject(ctx, local, key, 64<<20)
		if errors.Is(err, ErrNotFound) {
			raw, err = readObject(ctx, target, key, 64<<20)
		} else if err == nil {
			remove[key] = true
		}
		if errors.Is(err, ErrNotFound) && len(candidate) > 0 {
			sum := sha256.Sum256(candidate)
			if hex.EncodeToString(sum[:]) == expectedHash && int64(len(candidate)) == expectedSize {
				raw = candidate
				err = nil
				report.Recovered++
			}
		}
		if errors.Is(err, ErrNotFound) && allowMissing {
			report.Missing = append(report.Missing, key)
			return nil
		}
		if err != nil {
			return fmt.Errorf("missing artifact %s: %w", key, err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != expectedHash || int64(len(raw)) != expectedSize {
			return fmt.Errorf("source integrity check failed: %s", key)
		}
		if err = copyVerifiedObject(ctx, target, key, contentType, raw); err != nil {
			return err
		}
		report.Files++
		report.Bytes += int64(len(raw))
		return nil
	}
	datasets, err := store.Datasets()
	if err != nil {
		return report, err
	}
	for _, d := range datasets {
		if err = transfer("datasets/"+d.ID+".csv", "text/csv; charset=utf-8", d.SHA256, d.Size, nil); err != nil {
			return report, err
		}
	}
	for offset := 0; ; offset += 200 {
		runs, _, err := store.List("", "", "", 200, offset)
		if err != nil {
			return report, err
		}
		if len(runs) == 0 {
			break
		}
		for _, e := range runs {
			artifacts, err := store.Artifacts(e.ID)
			if err != nil {
				return report, err
			}
			if allowMissing {
				raw, err := json.MarshalIndent(e, "", "  ")
				if err != nil {
					return report, err
				}
				sum := sha256.Sum256(raw)
				key := "experiments/" + e.ID + "/migration/record-" + hex.EncodeToString(sum[:]) + ".json"
				if err = copyVerifiedObject(ctx, target, key, "application/json", raw); err != nil {
					return report, err
				}
				report.Records++
			}
			for _, a := range artifacts {
				if err = transfer("experiments/"+e.ID+"/"+a.Name, a.ContentType, a.SHA256, a.Size, exactArtifactCandidate(e, a, artifacts)); err != nil {
					return report, err
				}
			}
		}
	}
	clear := []ResearchDocument{}
	reader := &Server{Store: store, Objects: local, Config: Config{DataDir: root}}
	for offset := 0; ; offset += 200 {
		docs, err := store.ResearchDocuments("", "", 200, offset)
		if err != nil {
			return report, err
		}
		if len(docs) == 0 {
			break
		}
		for _, metadata := range docs {
			original, err := store.ResearchDocument(metadata.ID)
			if err != nil {
				return report, err
			}
			d := original
			if d.Content == "" {
				d, err = reader.loadDocument(ctx, d.ID)
				if errors.Is(err, ErrNotFound) {
					remote := &Server{Store: store, Objects: target}
					d, err = remote.loadDocument(ctx, metadata.ID)
				}
				if err != nil {
					return report, err
				}
			} else {
				clear = append(clear, d)
			}
			hash := sha256.Sum256(d.Evidence)
			if hex.EncodeToString(hash[:]) != d.EvidenceSHA256 {
				return report, errors.New("document evidence integrity check failed")
			}
			for name, raw := range map[string][]byte{documentSourceName(d): []byte(d.Content), "evidence.json": d.Evidence} {
				key := documentPrefix(d) + name
				if err = copyVerifiedObject(ctx, target, key, "text/plain; charset=utf-8", raw); err != nil {
					return report, err
				}
				report.Files++
				report.Bytes += int64(len(raw))
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key))); err == nil {
					remove[key] = true
				}
			}
			if len(d.AssetFiles) > 0 {
				remote := &Server{Objects: target}
				for _, asset := range d.AssetFiles {
					raw, err := reader.readDocumentAsset(ctx, d, asset)
					if errors.Is(err, ErrNotFound) {
						raw, err = remote.readDocumentAsset(ctx, d, asset)
					}
					if err != nil {
						return report, err
					}
					key := documentPrefix(d) + asset.Name
					if err = copyVerifiedObject(ctx, target, key, asset.ContentType, raw); err != nil {
						return report, err
					}
					report.Files++
					report.Bytes += int64(len(raw))
					if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key))); err == nil {
						remove[key] = true
					}
				}
				raw, _ := json.Marshal(documentFigureManifest{Figures: d.Figures, Assets: d.AssetFiles})
				key := documentPrefix(d) + "figures.json"
				if err = copyVerifiedObject(ctx, target, key, "application/json", raw); err != nil {
					return report, err
				}
				report.Files++
				report.Bytes += int64(len(raw))
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key))); err == nil {
					remove[key] = true
				}
			}

			report.Documents++
		}
	}
	if err = store.clearMigratedDocuments(clear); err != nil {
		return report, err
	}
	if removeLocal {
		for key := range remove {
			filename, _ := local.filename(key)
			if err = os.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
				return report, err
			}
			report.Removed++
		}
	}
	return report, nil
}

// Only a candidate whose bytes match the original recorded SHA-256 and size is
// accepted. This recovers representations of saved evidence, never new results.
func exactArtifactCandidate(e Experiment, a Artifact, all []Artifact) []byte {
	if e.Plan == nil || e.Result == nil {
		return nil
	}
	encode := func(v any) []byte { raw, _ := json.MarshalIndent(v, "", "  "); return raw }
	switch path.Base(a.Name) {
	case "experiment.py", "experiment.js", "experiment.R", "experiment.go", "experiment.cpp":
		return []byte(e.Plan.Code)
	case "plan.json":
		return encode(e.Plan)
	case "results.json":
		return encode(e.Result)
	case "input.json":
		if e.DatasetID == "" {
			return encode(WorkerInput{Plan: e.Plan})
		}
	case "data.csv":
		return []byte(e.Result.CSV)
	case "chart.svg":
		return []byte(renderSVG(*e.Result, e.Plan.Adapter))
	case "report.md":
		return []byte(renderReport(e, *e.Result))
	case "requirements.txt":
		deps := []string{"# Python " + e.Result.PythonVersion}
		for name, version := range e.Result.Dependencies {
			if name != "python" {
				deps = append(deps, name+"=="+version)
			}
		}
		sort.Strings(deps)
		return []byte(strings.Join(deps, "\n") + "\n")
	case "checksums.json":
		checks := map[string]string{}
		for _, artifact := range all {
			if path.Base(artifact.Name) != "checksums.json" {
				checks[path.Base(artifact.Name)] = artifact.SHA256
			}
		}
		return encode(checks)
	}
	return nil
}
