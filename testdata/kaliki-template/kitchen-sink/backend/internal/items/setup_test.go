package items_test

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/example/kitchen-sink-app/backend/internal/items"
	"github.com/example/kitchen-sink-app/backend/internal/testsupport"
)

var (
	testDB *testsupport.TestDB
	router *gin.Engine
)

func TestMain(m *testing.M) {
	testDB = testsupport.Connect("test_items")

	r, api := testsupport.NewRouter()
	items.New(testDB.Pool).Register(api)
	router = r

	os.Exit(m.Run())
}

func setupTest(t *testing.T) {
	t.Helper()
	testDB.Truncate(t)
}
