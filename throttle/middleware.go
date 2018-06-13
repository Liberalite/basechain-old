package throttle

import (
	"errors"
	"github.com/loomnetwork/loomchain"
	"github.com/loomnetwork/loomchain/auth"
	"github.com/loomnetwork/loomchain/log"
	"fmt"
)


func GetThrottleTxMiddleWare(maxAccessCount int64, sessionDuration int64) (loomchain.TxMiddlewareFunc) {
	th := NewThrottle(maxAccessCount, sessionDuration)
	return loomchain.TxMiddlewareFunc(func(
		state loomchain.State,
		txBytes []byte,
		next loomchain.TxHandlerFunc,
	) (res loomchain.TxHandlerResult, err error)  {

		origin := auth.Origin(state.Context())
		if origin.IsEmpty() {
			return res, errors.New("transaction has no origin")
		}

		limiterCtx, err := th.run(state.Context(), "ThrottleTxMiddleWare")

		if err != nil {
			log.Error(err.Error())
			return res, err
		}

		if limiterCtx.Reached {
			message := fmt.Sprintf("Out of access count for current session: %d out of %d, Try after sometime!",  limiterCtx.Limit - limiterCtx.Remaining, limiterCtx.Limit)
			log.Error(message)
			return res, errors.New(message)
		}

		return next(state, txBytes)
	})
}