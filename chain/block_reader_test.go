// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package chain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlockReader(t *testing.T) {
	ch := initChain()
	b0 := ch.GenesisBlock()

	b1, q1 := newBlock(b0, 2)
	ch.AddBlock(b1, q1)

	b2, q2 := newBlock(b1, 2)
	ch.AddBlock(b2, q2)

	b3, q3 := newBlock(b2, 2)
	ch.AddBlock(b3, q3)

	b4, q4 := newBlock(b3, 2)
	ch.AddBlock(b4, q4)

	br := ch.NewBlockReader(b2.ID())

	blks, err := br.Read()
	assert.Nil(t, err)
	assert.Equal(t, blks[0].ID(), b3.ID())
	assert.False(t, blks[0].Obsolete)

	blks, err = br.Read()
	assert.Nil(t, err)
	assert.Equal(t, blks[0].ID(), b4.ID())
	assert.False(t, blks[0].Obsolete)
}

func TestBlockReaderFork(t *testing.T) {
	ch := initChain()
	b0 := ch.GenesisBlock()

	b1, q1 := newBlock(b0, 1)
	ch.AddBlock(b1, q1)

	b2, q2 := newBlock(b1, 2)
	ch.AddBlock(b2, q2)

	b2x, q2x := newBlock(b1, 2)
	ch.AddBlock(b2x, q2x)

	b3, q3 := newBlock(b2, 3)
	ch.AddBlock(b3, q3)

	b4, q4 := newBlock(b3, 4)
	ch.AddBlock(b4, q4)

	br := ch.NewBlockReader(b2x.ID())

	blks, err := br.Read()
	assert.Nil(t, err)

	assert.Equal(t, len(blks), 1)
	assert.Equal(t, blks[0].Header().ID(), b3.Header().ID())
	assert.False(t, blks[0].Obsolete)
	// assert.Equal(t, blks[0].Header().ID(), b2.Header().ID())
	// assert.False(t, blks[0].Obsolete)

	blks, err = br.Read()
	assert.Nil(t, err)
	assert.Equal(t, blks[0].Header().ID(), b4.Header().ID())
	assert.False(t, blks[0].Obsolete)
}
