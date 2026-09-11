package engine

import (
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func genServiceWithScan(e schema.Entity, path string) string {

	return `


rows, err := repository.GetAll` + e.Name + `(db, limit, offset)
if err != nil {
return nil, err
}
defer rows.Close()

raw, err := dbscan.ScanRows(rows)
if err != nil {
return nil, err
}

return mapToDTO(e, raw), nil
`
}
