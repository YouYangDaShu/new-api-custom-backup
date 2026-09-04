package relay

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageHelperRejectsMissingFixedPriceBeforeUpstream(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamRequests.Add(1)
	}))
	t.Cleanup(upstream.Close)

	for name, mode := range map[string]int{
		"generation": relayconstant.RelayModeImagesGenerations,
		"edit":       relayconstant.RelayModeImagesEdits,
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, upstream.URL, nil)
			info := &relaycommon.RelayInfo{
				RelayMode: mode,
				Request:   &dto.ImageRequest{},
				PriceData: types.PriceData{UsePrice: false, ModelPrice: 0},
			}

			err := ImageHelper(c, info)
			require.NotNil(t, err)
			require.Equal(t, int32(0), upstreamRequests.Load())
		})
	}
}
