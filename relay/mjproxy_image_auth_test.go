package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The /mj/image/:id route is registered without TokenAuth, so the signed URL
// check is its only access control: anonymous, tampered, expired, and
// cross-task requests must all be rejected before any lookup or proxying.
func TestRelayMidjourneyImageRequiresValidSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))

	router := gin.New()
	router.GET("/mj/image/:id", RelayMidjourneyImage)

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(w, req)
		return w
	}

	const mjId = "1118138338562560102"
	future := time.Now().Add(time.Hour).Unix()
	sig := service.MjImageSignature(mjId, future)
	signedPath := func(id string) string {
		return "/mj/image/" + id + "?ex=" + strconv.FormatInt(future, 10) + "&sig=" + sig
	}

	// Anonymous request (the pre-fix shape) is rejected.
	assert.Equal(t, http.StatusForbidden, get("/mj/image/"+mjId).Code)
	// Signature for one task does not open another user's task.
	assert.Equal(t, http.StatusForbidden, get(signedPath("someone-elses-task")).Code)
	// Expired signature is rejected even when correctly signed.
	past := time.Now().Add(-time.Minute).Unix()
	expired := "/mj/image/" + mjId + "?ex=" + strconv.FormatInt(past, 10) + "&sig=" + service.MjImageSignature(mjId, past)
	assert.Equal(t, http.StatusForbidden, get(expired).Code)
	// A valid signature passes the auth gate; a missing task then 400s
	// (proving the check runs before, not instead of, the lookup).
	assert.Equal(t, http.StatusBadRequest, get(signedPath(mjId)).Code)
}
