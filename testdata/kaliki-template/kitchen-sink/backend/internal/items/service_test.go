package items_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/example/kitchen-sink-app/backend/internal/testsupport"
)

// One top-level test per OpenAPI operation, subtests in the conventional order.

func TestCreateItem(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/items", `{"name":"widget"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (%s)", w.Code, w.Body.String())
		}

		var got struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Name != "widget" {
			t.Fatalf("name = %q, want widget", got.Name)
		}

		// Verify DB state, not just the HTTP response.
		var count int
		if err := testDB.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM items WHERE id = $1`, got.ID).Scan(&count); err != nil {
			t.Fatalf("query: %v", err)
		}
		if count != 1 {
			t.Fatalf("rows = %d, want 1", count)
		}
	})

	t.Run("validation", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/items", `{}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

func TestListItems(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)

		testsupport.DoJSON(router, http.MethodPost, "/api/v1/items", `{"name":"a"}`)
		testsupport.DoJSON(router, http.MethodPost, "/api/v1/items", `{"name":"b"}`)

		w := testsupport.DoJSON(router, http.MethodGet, "/api/v1/items", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		var got struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got.Items) != 2 {
			t.Fatalf("items = %d, want 2", len(got.Items))
		}
	})
}

func TestGetItem(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)

		create := testsupport.DoJSON(router, http.MethodPost, "/api/v1/items", `{"name":"findme"}`)
		var created struct {
			ID uuid.UUID `json:"id"`
		}
		_ = json.Unmarshal(create.Body.Bytes(), &created)

		w := testsupport.DoJSON(router, http.MethodGet, "/api/v1/items/"+created.ID.String(), "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodGet, "/api/v1/items/"+uuid.NewString(), "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
	})

	t.Run("validation", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodGet, "/api/v1/items/not-a-uuid", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}
