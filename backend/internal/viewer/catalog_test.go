package viewer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDataQuery(t *testing.T) {
	shop := func(kind TableKind) relation {
		return relation{table: Table{Schema: "shop", Name: "orders", Kind: kind}}
	}

	tests := []struct {
		name string
		rel  relation
		cols []string
		pk   []string
		want string
	}{
		{
			name: "orders by primary key",
			rel:  shop(KindTable),
			cols: []string{"id", "total"},
			pk:   []string{"id"},
			want: `SELECT t."id"::text, t."total"::text FROM "shop"."orders" AS t ORDER BY t."id" LIMIT $1 OFFSET $2`,
		},
		{
			name: "composite primary key keeps its order",
			rel:  shop(KindTable),
			cols: []string{"a", "b"},
			pk:   []string{"b", "a"},
			want: `SELECT t."a"::text, t."b"::text FROM "shop"."orders" AS t ORDER BY t."b", t."a" LIMIT $1 OFFSET $2`,
		},
		{
			name: "table without primary key orders by ctid",
			rel:  shop(KindTable),
			cols: []string{"message"},
			want: `SELECT t."message"::text FROM "shop"."orders" AS t ORDER BY t.ctid LIMIT $1 OFFSET $2`,
		},
		{
			name: "view is not ordered",
			rel:  shop(KindView),
			cols: []string{"id"},
			want: `SELECT t."id"::text FROM "shop"."orders" AS t LIMIT $1 OFFSET $2`,
		},
		{
			name: "identifiers are quoted",
			rel:  relation{table: Table{Schema: `we"ird`, Name: "Weird Table", Kind: KindTable}},
			cols: []string{`Odd "Col"`},
			pk:   []string{`Odd "Col"`},
			want: `SELECT t."Odd ""Col"""::text FROM "we""ird"."Weird Table" AS t ORDER BY t."Odd ""Col""" LIMIT $1 OFFSET $2`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, dataQuery(tt.rel, tt.cols, tt.pk))
		})
	}
}