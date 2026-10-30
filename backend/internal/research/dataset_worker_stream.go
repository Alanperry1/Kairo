package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func (j *Jobs) streamDatasetRows(ctx context.Context, d Dataset, writer io.Writer) error {
	body, _, err := j.objectStorage().Get(ctx, "datasets/"+d.ID+".csv")
	if err != nil {
		return err
	}
	defer body.Close()
	hash := sha256.New()
	count := &byteCounter{}
	csvReader, guard := newCSVReader(io.TeeReader(&contextReader{ctx: ctx, reader: body}, io.MultiWriter(hash, count)))
	header, err := csvReader.Read()
	if err != nil {
		return err
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	encoder := json.NewEncoder(writer)
	rows := 0
	for {
		guard.used = 0
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		record := map[string]string{}
		for i, key := range header {
			record[key] = row[i]
		}
		if err = encoder.Encode(record); err != nil {
			return err
		}
		rows++
	}
	if hex.EncodeToString(hash.Sum(nil)) != d.SHA256 || count.n != d.Size || rows != d.Rows {
		return errors.New("streamed dataset integrity check failed")
	}
	return nil
}

const prepareStreamCode = `import csv, hashlib, json, sys
request=json.load(open('input.json'))
source=request['dataset']
sys.stdout.write('KAIRO_DATASET_STREAM\n'+json.dumps(request)+'\n')
checksum=hashlib.sha256()
with open('dataset.csv','rb') as raw:
    while chunk:=raw.read(1024*1024): checksum.update(chunk)
if checksum.hexdigest()!=source['sha256']: raise ValueError('Dataset checksum mismatch')
with open('dataset.csv',encoding='utf-8-sig',newline='') as rows:
    for row in csv.DictReader(rows): sys.stdout.write(json.dumps(row)+'\n')
`
