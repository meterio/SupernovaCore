// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package script

import (
	"github.com/meterio/meter-pov/script/accountlock"
	"github.com/meterio/meter-pov/script/auction"
	"github.com/meterio/meter-pov/script/staking"
)

const (
	STAKING_MODULE_NAME = string("staking")
	STAKING_MODULE_ID   = uint32(1000)

	AUCTION_MODULE_NAME = string("auction")
	AUCTION_MODULE_ID   = uint32(1001)

	ACCOUNTLOCK_MODULE_NAME = string("accountlock")
	ACCOUNTLOCK_MODULE_ID   = uint32(1002)
)

func ModuleStakingInit(se *ScriptEngine) *staking.Staking {
	stk := staking.NewStaking(se.chain, se.stateCreator)
	if stk == nil {
		panic("init staking module failed")
	}

	mod := &Module{
		modName:    STAKING_MODULE_NAME,
		modID:      STAKING_MODULE_ID,
		modPtr:     stk,
		modHandler: stk.Handle,
	}
	if err := se.modReg.Register(STAKING_MODULE_ID, mod); err != nil {
		panic("register staking module failed")
	}

	se.logger.Debug("ScriptEngine registered module", "mod", mod.modName)
	return stk
}

func ModuleAuctionInit(se *ScriptEngine) *auction.Auction {
	a := auction.NewAuction(se.chain, se.stateCreator)
	if a == nil {
		panic("init acution module failed")
	}

	mod := &Module{
		modName:    AUCTION_MODULE_NAME,
		modID:      AUCTION_MODULE_ID,
		modPtr:     a,
		modHandler: a.Handle,
	}
	if err := se.modReg.Register(AUCTION_MODULE_ID, mod); err != nil {
		panic("register auction module failed")
	}

	se.logger.Debug("ScriptEngine registered module", "mod", mod.modName)
	return a
}

func ModuleAccountLockInit(se *ScriptEngine) *accountlock.AccountLock {
	a := accountlock.NewAccountLock(se.chain, se.stateCreator)
	if a == nil {
		panic("init accountlock module failed")
	}

	mod := &Module{
		modName:    ACCOUNTLOCK_MODULE_NAME,
		modID:      ACCOUNTLOCK_MODULE_ID,
		modPtr:     a,
		modHandler: a.Handle,
	}
	if err := se.modReg.Register(ACCOUNTLOCK_MODULE_ID, mod); err != nil {
		panic("register accountlock module failed")
	}

	se.logger.Debug("ScriptEngine registered module", "mod", mod.modName)
	return a
}
