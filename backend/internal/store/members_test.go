package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type memberRow struct {
	fields []pgconn.FieldDescription
	values []string
}

func (r memberRow) FieldDescriptions() []pgconn.FieldDescription { return r.fields }
func (r memberRow) Values() ([]any, error)                       { return nil, nil }
func (r memberRow) RawValues() [][]byte                          { return nil }

func (r memberRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("expected %d destinations, got %d", len(r.values), len(dest))
	}
	for i, value := range r.values {
		target, ok := dest[i].(*string)
		if !ok {
			return fmt.Errorf("destination %d is not a string", i)
		}
		*target = value
	}
	return nil
}

func TestListMembersMapsAvatarURLColumn(t *testing.T) {
	if !strings.Contains(listMembersQuery, "coalesce(p.avatar_url,'') as avatar_url") {
		t.Fatal("list members query must alias coalesce result as avatar_url")
	}

	row := memberRow{
		fields: []pgconn.FieldDescription{{Name: "id"}, {Name: "email"}, {Name: "display_name"}, {Name: "avatar_url"}, {Name: "role"}},
		values: []string{"member-1", "ari@example.com", "Ari", "https://example.com/ari.png", "member"},
	}

	member, err := pgx.RowToStructByName[Member](row)
	if err != nil {
		t.Fatalf("map member row: %v", err)
	}
	if member.AvatarURL != "https://example.com/ari.png" {
		t.Fatalf("AvatarURL = %q, want avatar URL", member.AvatarURL)
	}
}
