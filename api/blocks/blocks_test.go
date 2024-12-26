// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package blocks_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meterio/supernova/api/blocks"
	meter_block "github.com/meterio/supernova/block"
	"github.com/stretchr/testify/assert"
)

const (
	testAddress = "56e81f171bcc55a6ff8345e692c0f86e5b48e01a"
	testPrivHex = "efa321f290811731036e5eccd373114e5186d9fe419081f5a607231279d5ef01"
)

var blk *meter_block.Block
var ts *httptest.Server

var invalidBytes32 = "0x000000000000000000000000000000000000000000000000000000000000000g" //invlaid bytes32
var invalidNumberRevision = "4294967296"                                                  //invalid block number

func TestBlock(t *testing.T) {
	defer ts.Close()
	//invalid block id
	res, statusCode := httpGet(t, ts.URL+"/blocks/"+invalidBytes32)
	assert.Equal(t, http.StatusBadRequest, statusCode)
	//invalid block number
	res, statusCode = httpGet(t, ts.URL+"/blocks/"+invalidNumberRevision)
	assert.Equal(t, http.StatusBadRequest, statusCode)

	res, statusCode = httpGet(t, ts.URL+"/blocks/"+blk.ID().String())
	rb := new(blocks.JSONBlockSummary)
	if err := json.Unmarshal(res, &rb); err != nil {
		t.Fatal(err)
	}
	checkBlock(t, blk, rb)
	assert.Equal(t, http.StatusOK, statusCode)

	res, statusCode = httpGet(t, ts.URL+"/blocks/1")
	if err := json.Unmarshal(res, &rb); err != nil {
		t.Fatal(err)
	}
	checkBlock(t, blk, rb)
	assert.Equal(t, http.StatusOK, statusCode)

	res, statusCode = httpGet(t, ts.URL+"/blocks/best")
	if err := json.Unmarshal(res, &rb); err != nil {
		t.Fatal(err)
	}
	checkBlock(t, blk, rb)
	assert.Equal(t, http.StatusOK, statusCode)
}

func checkBlock(t *testing.T, expBl *meter_block.Block, actBl *blocks.JSONBlockSummary) {
	header := expBl.Header()
	assert.Equal(t, header.Number(), actBl.Number, "Number should be equal")
	assert.Equal(t, header.ID(), actBl.ID, "Hash should be equal")

}

func httpGet(t *testing.T, url string) ([]byte, int) {
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	r, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return r, res.StatusCode
}
