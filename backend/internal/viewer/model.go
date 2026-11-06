package viewer

type TableKind string

const (
	KindTable            TableKind = "table"
	KindPartitionedTable TableKind = "partitioned table"
	KindView             TableKind = "view"
	KindMaterializedView TableKind = "materialized view"
	KindForeignTable     TableKind = "foreign table"
)

type Table struct {
	Schema string
	Name   string
	Kind   TableKind
}

type Column struct {
	Name     string
	Type     string
	Nullable bool
	Default  *string
}

type Constraint struct {
	Name       string
	Type       string
	Definition string
}

type Index struct {
	Name       string
	Definition string
	Unique     bool
	Primary    bool
}

type TableDetail struct {
	Table
	Columns     []Column
	Constraints []Constraint
	Indexes     []Index
}

type TablePage struct {
	Columns  []string
	Rows     [][]*string
	Page     int
	PageSize int
	HasNext  bool
}