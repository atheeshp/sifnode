package keeper

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/Sifchain/sifnode/x/clp/types"
)

func CalcSwapPmtp(toRowan bool, y, pmtpCurrentRunningRate sdk.Dec) sdk.Dec {
	// if pmtpCurrentRunningRate.IsNil() {
	// 	if toRowan {
	// 		return y.Quo(sdk.NewDec(1))
	// 	}
	// 	return y.Mul(sdk.NewDec(1))
	// }
	if toRowan {
		return y.Quo(sdk.NewDec(1).Add(pmtpCurrentRunningRate))
	}
	return y.Mul(sdk.NewDec(1).Add(pmtpCurrentRunningRate))
}

// More details on the formula
// https://github.com/Sifchain/sifnode/blob/develop/docs/1.Liquidity%20Pools%20Architecture.md
func CalculateWithdrawal(poolUnits sdk.Uint, nativeAssetBalance string,
	externalAssetBalance string, lpUnits string, wBasisPoints string, asymmetry sdk.Int) (sdk.Uint, sdk.Uint, sdk.Uint, sdk.Uint) {
	poolUnitsF := sdk.NewDecFromBigInt(poolUnits.BigInt())

	nativeAssetBalanceF, err := sdk.NewDecFromStr(nativeAssetBalance)
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", nativeAssetBalance, err))
	}
	externalAssetBalanceF, err := sdk.NewDecFromStr(externalAssetBalance)
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", externalAssetBalance, err))
	}
	lpUnitsF, err := sdk.NewDecFromStr(lpUnits)
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", lpUnits, err))
	}
	wBasisPointsF, err := sdk.NewDecFromStr(wBasisPoints)
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", wBasisPoints, err))
	}
	asymmetryF, err := sdk.NewDecFromStr(asymmetry.String())
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", asymmetry.String(), err))
	}
	denominator := sdk.NewDec(10000).Quo(wBasisPointsF)
	unitsToClaim := lpUnitsF.Quo(denominator)
	withdrawExternalAssetAmount := externalAssetBalanceF.Quo(poolUnitsF.Quo(unitsToClaim))
	withdrawNativeAssetAmount := nativeAssetBalanceF.Quo(poolUnitsF.Quo(unitsToClaim))

	swapAmount := sdk.NewDec(0)
	//if asymmetry is positive we need to swap from native to external
	if asymmetry.IsPositive() {
		unitsToSwap := unitsToClaim.Quo(sdk.NewDec(10000).Quo(asymmetryF.Abs()))
		swapAmount = nativeAssetBalanceF.Quo(poolUnitsF.Quo(unitsToSwap))
	}
	//if asymmetry is negative we need to swap from external to native
	if asymmetry.IsNegative() {
		unitsToSwap := unitsToClaim.Quo(sdk.NewDec(10000).Quo(asymmetryF.Abs()))
		swapAmount = externalAssetBalanceF.Quo(poolUnitsF.Quo(unitsToSwap))
	}

	//if asymmetry is 0 we don't need to swap
	lpUnitsLeft := lpUnitsF.Sub(unitsToClaim)

	return sdk.NewUintFromBigInt(withdrawNativeAssetAmount.RoundInt().BigInt()),
		sdk.NewUintFromBigInt(withdrawExternalAssetAmount.RoundInt().BigInt()),
		sdk.NewUintFromBigInt(lpUnitsLeft.RoundInt().BigInt()),
		sdk.NewUintFromBigInt(swapAmount.RoundInt().BigInt())
}

// More details on the formula
// https://github.com/Sifchain/sifnode/blob/develop/docs/1.Liquidity%20Pools%20Architecture.md
func CalculateWithdrawalFromUnits(poolUnits sdk.Uint, nativeAssetBalance string,
	externalAssetBalance string, lpUnits string, withdrawUnits sdk.Uint) (sdk.Uint, sdk.Uint, sdk.Uint) {
	poolUnitsF := sdk.NewDecFromBigInt(poolUnits.BigInt())

	nativeAssetBalanceF, err := sdk.NewDecFromStr(nativeAssetBalance)
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", nativeAssetBalance, err))
	}
	externalAssetBalanceF, err := sdk.NewDecFromStr(externalAssetBalance)
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", externalAssetBalance, err))
	}
	lpUnitsF, err := sdk.NewDecFromStr(lpUnits)
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", lpUnits, err))
	}
	withdrawUnitsF, err := sdk.NewDecFromStr(withdrawUnits.String())
	if err != nil {
		panic(fmt.Errorf("fail to convert %s to cosmos.Dec: %w", withdrawUnits, err))
	}

	withdrawExternalAssetAmount := externalAssetBalanceF.Quo(poolUnitsF.Quo(withdrawUnitsF))
	withdrawNativeAssetAmount := nativeAssetBalanceF.Quo(poolUnitsF.Quo(withdrawUnitsF))

	//if asymmetry is 0 we don't need to swap
	lpUnitsLeft := lpUnitsF.Sub(withdrawUnitsF)

	return sdk.NewUintFromBigInt(withdrawNativeAssetAmount.RoundInt().BigInt()),
		sdk.NewUintFromBigInt(withdrawExternalAssetAmount.RoundInt().BigInt()),
		sdk.NewUintFromBigInt(lpUnitsLeft.RoundInt().BigInt())
}

// Calculate pool units taking into account the current pmtpCurrentRunningRate
// R - native asset balance
// A - external asset balance
// r - native asset amount
// a - external asset amount
// P - current number of pool units
// #################
// TODO: need to check we're not exceeding liquidity protection OR swap block switches
// TODO: replace original with V2
// ################
func CalculatePoolUnitsV2(P, R, A, r, a sdk.Uint, swapFeeRate, pmtpCurrentRunningRate sdk.Dec) (sdk.Uint, sdk.Uint, error) {
	pmtpCurrentRunningRateR := DecToRat(&pmtpCurrentRunningRate)
	swapFeeRateR := DecToRat(&swapFeeRate)

	symmetryState := GetLiquidityAddSymmetryState(A, a, R, r)
	switch symmetryState {
	case ErrorEmptyPool:
		// At least one side of the pool is empty.
		//
		// If both sides of the pool are empty then we start counting pool units from scratch. We can assign
		// an arbitrary number, which we'll choose to be the amount of native asset added. However this
		// should only be done if adding to both sides of the pool, otherwise one side will still be empty.
		//
		// If only one side of the pool is empty then it's not clear what should be done - in which case
		// we'll default to doing the same thing.

		if a.IsZero() || r.IsZero() {
			return sdk.Uint{}, sdk.Uint{}, types.ErrInValidAmount
		}

		return r, r, nil
	case ErrorNothingAdded:
		// Keep the pool units as they were and don't give any units to the liquidity provider
		return P, sdk.ZeroUint(), nil
	case NeedMoreY:
		// Need more native token to make R/A == r/a
		swapAmount := CalculateExternalSwapAmountAsymmetric(R, A, r, a, &swapFeeRateR, &pmtpCurrentRunningRateR)
		aCorrected := a.Sub(swapAmount)
		AProjected := A.Add(swapAmount)

		// external or native asset can be used to calculate pool units since now r/R = a/A. for convenience
		// use external asset
		poolUnits, lpUnits := CalculatePoolUnitsSymmetric(AProjected, aCorrected, P)
		return poolUnits, lpUnits, nil
	case Symmetric:
		// R/A == r/a
		poolUnits, lpUnits := CalculatePoolUnitsSymmetric(R, r, P)
		return poolUnits, lpUnits, nil
	case NeedMoreX:
		// Need more external token to make R/A == r/a
		swapAmount := CalculateNativeSwapAmountAsymmetric(R, A, r, a, &swapFeeRateR, &pmtpCurrentRunningRateR)
		rCorrected := r.Sub(swapAmount)
		RProjected := R.Add(swapAmount)
		poolUnits, lpUnits := CalculatePoolUnitsSymmetric(RProjected, rCorrected, P)
		return poolUnits, lpUnits, nil
	default:
		panic("Expect not to reach here!")
	}
}

func CalculatePoolUnitsSymmetric(X, x, P sdk.Uint) (sdk.Uint, sdk.Uint) {
	var providerUnitsB big.Int

	providerUnitsB.Mul(x.BigInt(), P.BigInt()).Quo(&providerUnitsB, X.BigInt()) // providerUnits = P * x / X
	providerUnits := sdk.NewUintFromBigInt(&providerUnitsB)

	return P.Add(providerUnits), providerUnits
}

const (
	ErrorEmptyPool = iota
	ErrorNothingAdded
	NeedMoreY // Need more y token to make Y/X == y/x
	Symmetric // Y/X == y/x
	NeedMoreX // Need more x token to make Y/X == y/x
)

// Determines how the amount of assets added to a pool, x, y, compare to the current
// pool ratio, Y/X
func GetLiquidityAddSymmetryState(X, x, Y, y sdk.Uint) int {
	if X.IsZero() || Y.IsZero() {
		return ErrorEmptyPool
	}

	if x.IsZero() && y.IsZero() {
		return ErrorNothingAdded
	}

	if x.IsZero() {
		return NeedMoreX
	}
	var YoverX, yOverx big.Rat

	YoverX.SetFrac(Y.BigInt(), X.BigInt())
	yOverx.SetFrac(y.BigInt(), x.BigInt())

	switch YoverX.Cmp(&yOverx) {
	case -1:
		return NeedMoreX
	case 0:
		return Symmetric
	case 1:
		return NeedMoreY
	default:
		panic("Expect not to reach here!")
	}
}

// More details on the formula
// https://github.com/Sifchain/sifnode/blob/develop/docs/1.Liquidity%20Pools%20Architecture.md

//native asset balance  : currently in pool before adding
//external asset balance : currently in pool before adding
//native asset to added  : the amount the user sends
//external asset amount to be added : the amount the user sends

// R = native Balance (before)
// A = external Balance (before)
// r = native asset added;
// a = external asset added
// P = existing Pool Units
// slipAdjustment = (1 - ABS((R a - r A)/((r + R) (a + A))))
// units = ((P (a R + A r))/(2 A R))*slidAdjustment

func CalculatePoolUnits(oldPoolUnits, nativeAssetBalance, externalAssetBalance, nativeAssetAmount,
	externalAssetAmount sdk.Uint, externalDecimals uint8, symmetryThreshold, ratioThreshold sdk.Dec) (sdk.Uint, sdk.Uint, error) {

	if nativeAssetAmount.IsZero() && externalAssetAmount.IsZero() {
		return sdk.ZeroUint(), sdk.ZeroUint(), types.ErrAmountTooLow
	}

	if nativeAssetBalance.Add(nativeAssetAmount).IsZero() {
		return sdk.ZeroUint(), sdk.ZeroUint(), errors.Wrap(errors.ErrInsufficientFunds, nativeAssetAmount.String())
	}
	if externalAssetBalance.Add(externalAssetAmount).IsZero() {
		return sdk.ZeroUint(), sdk.ZeroUint(), errors.Wrap(errors.ErrInsufficientFunds, externalAssetAmount.String())
	}
	if nativeAssetBalance.IsZero() || externalAssetBalance.IsZero() {
		return nativeAssetAmount, nativeAssetAmount, nil
	}

	slipAdjustmentValues := calculateSlipAdjustment(nativeAssetBalance.BigInt(), externalAssetBalance.BigInt(),
		nativeAssetAmount.BigInt(), externalAssetAmount.BigInt())

	one := big.NewRat(1, 1)
	symmetryThresholdRat := DecToRat(&symmetryThreshold)

	var diff big.Rat
	diff.Sub(one, slipAdjustmentValues.slipAdjustment)
	if diff.Cmp(&symmetryThresholdRat) == 1 { // this is: if diff > symmetryThresholdRat
		return sdk.ZeroUint(), sdk.ZeroUint(), types.ErrAsymmetricAdd
	}

	ratioThresholdRat := DecToRat(&ratioThreshold)
	normalisingFactor := CalcDenomChangeMultiplier(externalDecimals, types.NativeAssetDecimals)
	ratioThresholdRat.Mul(&ratioThresholdRat, &normalisingFactor)
	ratioDiff, err := CalculateRatioDiff(externalAssetBalance.BigInt(), nativeAssetBalance.BigInt(), externalAssetAmount.BigInt(), nativeAssetAmount.BigInt())
	if err != nil {
		return sdk.ZeroUint(), sdk.ZeroUint(), err
	}
	if ratioDiff.Cmp(&ratioThresholdRat) == 1 { //if ratioDiff > ratioThreshold
		return sdk.ZeroUint(), sdk.ZeroUint(), types.ErrAsymmetricRatioAdd
	}

	stakeUnits := calculateStakeUnits(oldPoolUnits.BigInt(), nativeAssetBalance.BigInt(),
		externalAssetBalance.BigInt(), nativeAssetAmount.BigInt(), slipAdjustmentValues)

	var newPoolUnit big.Int
	newPoolUnit.Add(oldPoolUnits.BigInt(), stakeUnits)

	return sdk.NewUintFromBigInt(&newPoolUnit), sdk.NewUintFromBigInt(stakeUnits), nil
}

// | A/R - a/r |
func CalculateRatioDiff(A, R, a, r *big.Int) (big.Rat, error) {
	if R.Cmp(big.NewInt(0)) == 0 || r.Cmp(big.NewInt(0)) == 0 { // check for zeros
		return *big.NewRat(0, 1), types.ErrAsymmetricRatioAdd
	}
	var AdivR, adivr, diff big.Rat

	AdivR.SetFrac(A, R)
	adivr.SetFrac(a, r)
	diff.Sub(&AdivR, &adivr)
	diff.Abs(&diff)

	return diff, nil
}

// units = ((P (a R + A r))/(2 A R))*slidAdjustment
func calculateStakeUnits(P, R, A, r *big.Int, slipAdjustmentValues *slipAdjustmentValues) *big.Int {
	var add, numerator big.Int
	add.Add(slipAdjustmentValues.RTimesa, slipAdjustmentValues.rTimesA)
	numerator.Mul(P, &add)

	var denominator big.Int
	denominator.Mul(big.NewInt(2), A)
	denominator.Mul(&denominator, R)

	var n, d, stakeUnits big.Rat
	n.SetInt(&numerator)
	d.SetInt(&denominator)
	stakeUnits.Quo(&n, &d)
	stakeUnits.Mul(&stakeUnits, slipAdjustmentValues.slipAdjustment)

	return RatIntQuo(&stakeUnits)
}

// slipAdjustment = (1 - ABS((R a - r A)/((r + R) (a + A))))
type slipAdjustmentValues struct {
	slipAdjustment *big.Rat
	RTimesa        *big.Int
	rTimesA        *big.Int
}

func calculateSlipAdjustment(R, A, r, a *big.Int) *slipAdjustmentValues {
	var denominator, rPlusR, aPlusA big.Int
	rPlusR.Add(r, R)
	aPlusA.Add(a, A)
	denominator.Mul(&rPlusR, &aPlusA)

	var RTimesa, rTimesA, nominator big.Int
	RTimesa.Mul(R, a)
	rTimesA.Mul(r, A)
	nominator.Sub(&RTimesa, &rTimesA)

	var one, nom, denom, slipAdjustment big.Rat
	one.SetInt64(1)

	nom.SetInt(&nominator)
	denom.SetInt(&denominator)

	slipAdjustment.Quo(&nom, &denom)
	slipAdjustment.Abs(&slipAdjustment)
	slipAdjustment.Sub(&one, &slipAdjustment)

	return &slipAdjustmentValues{slipAdjustment: &slipAdjustment, RTimesa: &RTimesa, rTimesA: &rTimesA}
}

func CalcLiquidityFee(toRowan bool, X, x, Y sdk.Uint, swapFeeRate, pmtpCurrentRunningRate sdk.Dec) sdk.Uint {
	if IsAnyZero([]sdk.Uint{X, x, Y}) {
		return sdk.ZeroUint()
	}

	Xb := X.BigInt()
	xb := x.BigInt()
	Yb := Y.BigInt()
	rawXYK := calcRawXYK(xb, Xb, Yb)

	var fee big.Rat
	f := DecToRat(&swapFeeRate)
	fee.Mul(&f, &rawXYK)

	pmtpFac := calcPmtpFactor(pmtpCurrentRunningRate)

	if toRowan {
		fee.Quo(&fee, &pmtpFac) // res = y / pmtpFac
	} else {
		fee.Mul(&fee, &pmtpFac) // res = y * pmtpFac
	}

	return sdk.NewUintFromBigInt(RatIntQuo(&fee))
}

func CalcSwapResult(toRowan bool,
	X, x, Y sdk.Uint,
	pmtpCurrentRunningRate, swapFeeRate sdk.Dec) sdk.Uint {

	if IsAnyZero([]sdk.Uint{X, x, Y}) {
		return sdk.ZeroUint()
	}

	swapFeeRateR := DecToRat(&swapFeeRate)

	y := calcSwap(x.BigInt(), X.BigInt(), Y.BigInt(), &swapFeeRateR)
	pmtpFac := calcPmtpFactor(pmtpCurrentRunningRate)

	var res big.Rat
	if toRowan {
		res.Quo(&y, &pmtpFac) // res = y / pmtpFac
	} else {
		res.Mul(&y, &pmtpFac) // res = y * pmtpFac
	}

	num := RatIntQuo(&res)
	return sdk.NewUintFromBigInt(num)
}

// y = (1-f)*x*Y/(x+X)
func calcSwap(x, X, Y *big.Int, swapFeeRate *big.Rat) big.Rat {
	var diff big.Rat
	one := big.NewRat(1, 1)
	diff.Sub(one, swapFeeRate) // diff = 1 - f

	rawYXK := calcRawXYK(x, X, Y)
	diff.Mul(&diff, &rawYXK)

	return diff
}

func calcRawXYK(x, X, Y *big.Int) big.Rat {
	var numerator, denominator, xR, XR, YR, y big.Rat

	xR.SetInt(x)
	XR.SetInt(X)
	YR.SetInt(Y)
	numerator.Mul(&xR, &YR)   // x * Y
	denominator.Add(&XR, &xR) // X + x

	y.Quo(&numerator, &denominator) // y = (x * Y) / (X + x)

	return y
}

func calcPmtpFactor(r sdk.Dec) big.Rat {
	rRat := DecToRat(&r)
	one := big.NewRat(1, 1)

	one.Add(one, &rRat)

	return *one
}

func CalcSpotPriceNative(pool *types.Pool, decimalsExternal uint8, pmtpCurrentRunningRate sdk.Dec) (sdk.Dec, error) {
	return CalcSpotPriceX(pool.NativeAssetBalance, pool.ExternalAssetBalance, types.NativeAssetDecimals, decimalsExternal, pmtpCurrentRunningRate, true)
}

func CalcSpotPriceExternal(pool *types.Pool, decimalsExternal uint8, pmtpCurrentRunningRate sdk.Dec) (sdk.Dec, error) {
	return CalcSpotPriceX(pool.ExternalAssetBalance, pool.NativeAssetBalance, decimalsExternal, types.NativeAssetDecimals, pmtpCurrentRunningRate, false)
}

// Calculates the spot price of asset X in the preferred denominations accounting for PMTP.
// Since this method applies PMTP adjustment, one of X, Y must be the native asset.
func CalcSpotPriceX(X, Y sdk.Uint, decimalsX, decimalsY uint8, pmtpCurrentRunningRate sdk.Dec, isXNative bool) (sdk.Dec, error) {
	if X.Equal(sdk.ZeroUint()) {
		return sdk.ZeroDec(), types.ErrInValidAmount
	}

	var price big.Rat
	price.SetFrac(Y.BigInt(), X.BigInt())

	pmtpFac := calcPmtpFactor(pmtpCurrentRunningRate)
	var pmtpPrice big.Rat
	if isXNative {
		pmtpPrice.Mul(&price, &pmtpFac) // pmtpPrice = price * pmtpFac
	} else {
		pmtpPrice.Quo(&price, &pmtpFac) // pmtpPrice = price / pmtpFac
	}

	dcm := CalcDenomChangeMultiplier(decimalsX, decimalsY)
	pmtpPrice.Mul(&pmtpPrice, &dcm)

	return RatToDec(&pmtpPrice)
}
func CalcRowanValue(pool *types.Pool, pmtpCurrentRunningRate sdk.Dec, rowanAmount sdk.Uint) (sdk.Uint, error) {
	spotPrice, err := CalcRowanSpotPrice(pool, pmtpCurrentRunningRate)
	if err != nil {
		return sdk.ZeroUint(), err
	}
	value := spotPrice.Mul(sdk.NewDecFromBigInt(rowanAmount.BigInt()))
	return sdk.NewUintFromBigInt(value.RoundInt().BigInt()), nil
}

// Calculates spot price of Rowan accounting for PMTP
func CalcRowanSpotPrice(pool *types.Pool, pmtpCurrentRunningRate sdk.Dec) (sdk.Dec, error) {
	rowanBalance := sdk.NewDecFromBigInt(pool.NativeAssetBalance.BigInt())
	if rowanBalance.Equal(sdk.ZeroDec()) {
		return sdk.ZeroDec(), types.ErrInValidAmount
	}
	externalAssetBalance := sdk.NewDecFromBigInt(pool.ExternalAssetBalance.BigInt())
	unadjusted := externalAssetBalance.Quo(rowanBalance)
	return unadjusted.Mul(pmtpCurrentRunningRate.Add(sdk.OneDec())), nil
}

// Denom change multiplier = 10**decimalsX / 10**decimalsY
func CalcDenomChangeMultiplier(decimalsX, decimalsY uint8) big.Rat {
	diff := Abs(int16(decimalsX) - int16(decimalsY))
	dec := big.NewInt(1).Exp(big.NewInt(10), big.NewInt(int64(diff)), nil) // 10**|decimalsX - decimalsY|

	var res big.Rat
	if decimalsX > decimalsY {
		return *res.SetInt(dec)
	}
	return *res.SetFrac(big.NewInt(1), dec)
}

func calcPriceImpact(X, x sdk.Uint) sdk.Uint {
	if x.IsZero() {
		return sdk.ZeroUint()
	}

	Xb := X.BigInt()
	xb := x.BigInt()

	var d, q big.Int
	d.Add(xb, Xb)
	q.Quo(xb, &d) // q = x / (x + X)

	return sdk.NewUintFromBigInt(&q)
}

func CalculateAllAssetsForLP(pool types.Pool, lp types.LiquidityProvider) (sdk.Uint, sdk.Uint, sdk.Uint, sdk.Uint) {
	poolUnits := pool.PoolUnits
	nativeAssetBalance := pool.NativeAssetBalance
	externalAssetBalance := pool.ExternalAssetBalance
	return CalculateWithdrawal(
		poolUnits,
		nativeAssetBalance.String(),
		externalAssetBalance.String(),
		lp.LiquidityProviderUnits.String(),
		sdk.NewInt(types.MaxWbasis).String(),
		sdk.ZeroInt(),
	)
}

// Calculates how much external asset to swap for an asymmetric add to become
// symmetric.
// R - native asset balance
// A - external asset balance
// r - native asset amount
// a - external asset amount
// f - swap fee rate
// p - pmtp (ratio shifting) current running rate
//
// Calculates the amount of external asset to swap, s, such that the ratio of the added assets after the swap
// equals the ratio of assets in the pool after the swap i.e. calculates s, such that (a+A)/(r+R) = (a−s) / (r + s*R/(s+A)*(1−f)/(1+p)).
//
// Solving for s gives, s = math.Abs((math.Sqpt(R*(-1*(a+A))*(-1*f*f*a*R-f*f*A*R-2*f*p*a*R+4*f*p*A*r+2*f*p*A*R+4*f*A*r+4*f*A*R-p*p*a*R-p*p*A*R-4*p*A*r-4*p*A*R-4*A*r-4*A*R)) + f*a*R + f*A*R + p*a*R - 2*p*A*r - p*A*R - 2*A*r - 2*A*R) / (2 * (p + 1) * (r + R))).
//
// This function should only be used when when more native asset is required in order for an add to be symmetric i.e. when R,A,a > 0 and R/A > r/a.
// If more external asset is required, then due to ratio shifting the swap formula changes, in which case
// use CalculateNativeSwapAmountAsymmetric.
func CalculateExternalSwapAmountAsymmetric(R, A, r, a sdk.Uint, f, p *big.Rat) sdk.Uint {
	var RRat, ARat, rRat, aRat big.Rat
	RRat.SetInt(R.BigInt())
	ARat.SetInt(A.BigInt())
	rRat.SetInt(r.BigInt())
	aRat.SetInt(a.BigInt())

	s := CalculateExternalSwapAmountAsymmetricRat(&RRat, &ARat, &rRat, &aRat, f, p)
	return sdk.NewUintFromBigInt(RatIntQuo(&s))
}

// NOTE: this method is only exported to make testing easier
//
// NOTE: this method panics if a negative value is passed to the sqrt
// It's not clear whether this condition could ever happen given the external
// constraints on the inputs (e.g. X,Y,x > 0 and Y/X > y/x). It is possible to guard against
// a panic by ensuring the sqrt argument is positive.
func CalculateExternalSwapAmountAsymmetricRat(Y, X, y, x, f, r *big.Rat) big.Rat {
	var a_, b_, c_, d_, e_, f_, g_, h_, i_, j_, k_, l_, m_, n_, o_, p_, q_, r_, s_, t_, u_, v_, w_, x_, y_, z_, aa_, ab_, ac_, ad_, minusOne, one, two, four, r1 big.Rat
	minusOne.SetInt64(-1)
	one.SetInt64(1)
	two.SetInt64(2)
	four.SetInt64(4)
	r1.Add(r, &one)

	a_.Add(x, X)           // a_ = x + X
	b_.Mul(&a_, &minusOne) // b_ = -1 * (x + X)
	c_.Mul(Y, &b_)         // c_ = Y * -1 * (x + X)

	d_.Mul(f, f).Mul(&d_, x).Mul(&d_, Y)                 // d_ = f * f * x * Y
	e_.Mul(f, f).Mul(&e_, X).Mul(&e_, Y)                 // e_ := f * f * X * Y
	f_.Mul(&two, f).Mul(&f_, r).Mul(&f_, x).Mul(&f_, Y)  // f_ := 2 * f * r * x * Y
	g_.Mul(&four, f).Mul(&g_, r).Mul(&g_, X).Mul(&g_, y) // g_ := 4 * f * r * X * y
	h_.Mul(&two, f).Mul(&h_, r).Mul(&h_, X).Mul(&h_, Y)  // h_ := 2 * f * r * X * Y
	i_.Mul(&four, f).Mul(&i_, X).Mul(&i_, y)             // i_ := 4 * f * X * y
	j_.Mul(&four, f).Mul(&j_, X).Mul(&j_, Y)             // j_ := 4 * f * X * Y
	k_.Mul(r, r).Mul(&k_, x).Mul(&k_, Y)                 // k_ := r * r * x * Y
	l_.Mul(r, r).Mul(&l_, X).Mul(&l_, Y)                 // l_ := r * r * X * Y
	m_.Mul(&four, r).Mul(&m_, X).Mul(&m_, y)             // m_ := 4 * r * X * y
	n_.Mul(&four, r).Mul(&n_, X).Mul(&n_, Y)             // n_ := 4 * r * X * Y
	o_.Mul(&four, X).Mul(&o_, y)                         // o_ := 4 * X * y
	p_.Mul(&four, X).Mul(&p_, Y)                         // p_ := 4 * X * Y
	q_.Mul(f, x).Mul(&q_, Y)                             // q_ := f * x * Y
	r_.Mul(f, X).Mul(&r_, Y)                             // r_ := f * X * Y
	s_.Mul(r, x).Mul(&s_, Y)                             // s_ := r * x * Y
	t_.Mul(&two, r).Mul(&t_, X).Mul(&t_, y)              // t_ := 2 * r * X * y
	u_.Mul(r, X).Mul(&u_, Y)                             // u_ := r * X * Y
	v_.Mul(&two, X).Mul(&v_, y)                          // v_ := 2 * X * y
	w_.Mul(&two, X).Mul(&w_, Y)                          // w_ := 2 * X * Y

	x_.Add(y, Y) // x_ := (y + Y)

	y_.Add(&g_, &h_).Add(&y_, &i_).Add(&y_, &j_).Sub(&y_, &d_).Sub(&y_, &e_).Sub(&y_, &f_).Sub(&y_, &k_).Sub(&y_, &l_).Sub(&y_, &m_).Sub(&y_, &n_).Sub(&y_, &o_).Sub(&y_, &p_) // y_ :=  g_ + h_ + i_ + j_ - d_ - e_ - f_ - k_ - l_ - m_ - n_ - o_ - p_ // y_ := -d_ - e_ - f_ + g_ + h_ + i_ + j_ - k_ - l_ - m_ - n_ - o_ - p_

	z_.Mul(&c_, &y_)                     // z_ := c_ * y_
	aa_.SetInt(ApproxRatSquareRoot(&z_)) // aa_ := math.Sqrt(z_)

	ab_.Add(&aa_, &q_).Add(&ab_, &r_).Add(&ab_, &s_).Sub(&ab_, &t_).Sub(&ab_, &u_).Sub(&ab_, &v_).Sub(&ab_, &w_) // ab_ := (aa_ + q_ + r_ + s_ - t_ - u_ - v_ - w_)

	ac_.Mul(&two, &r1).Mul(&ac_, &x_) // ac_ := (2 * r1 * x_)
	ad_.Quo(&ab_, &ac_)               // ad_ := ab_ / ac_
	return *ad_.Abs(&ad_)
}

// Calculates how much native asset to swap for an asymmetric add to become
// symmetric.
// R - native asset balance
// A - external asset balance
// r - native asset amount
// a - external asset amount
// f - swap fee rate
// p - pmtp (ratio shifting) current running rate
//
// Calculates the amount of native asset to swap, s, such that the ratio of the added assets after the swap
// equals the ratio of assets in the pool after the swap i.e. calculates s, such that (r+R)/(a+A) = (r-s) / (a + (s*A)/(s+R)*(1+p)*(1-f)).
//
// Solving for s gives, s = math.Abs((math.Sqpt(math.Pow((-1*f*p*A*r-f*p*A*R-f*A*r-f*A*R+p*A*r+p*A*R+2*a*R+2*A*R), 2)-4*(a+A)*(a*R*R-A*r*R)) + f*p*A*r + f*p*A*R + f*A*r + f*A*R - p*A*r - p*A*R - 2*a*R - 2*A*R) / (2 * (a + A))).

// This function should only be used when when more external asset is required in order for an add to be symmetric i.e. when R,A,r > 0 and (a==0 or R/A < r/a)
// If more native asset is required, then due to ratio shifting the swap formula changes, in which case
// use CalculateExternalSwapAmountAsymmetric.
func CalculateNativeSwapAmountAsymmetric(R, A, r, a sdk.Uint, f, p *big.Rat) sdk.Uint {
	var RRat, ARat, rRat, aRat big.Rat
	RRat.SetInt(R.BigInt())
	ARat.SetInt(A.BigInt())
	rRat.SetInt(r.BigInt())
	aRat.SetInt(a.BigInt())

	s := CalculateNativeSwapAmountAsymmetricRat(&RRat, &ARat, &rRat, &aRat, f, p)
	return sdk.NewUintFromBigInt(RatIntQuo(&s))
}

// NOTE: this method is only exported to make testing easier
//
// NOTE: this method panics if a negative value is passed to the sqrt
// It's not clear whether this condition could ever happen given the
// constraints on the inputs (i.e. Y,X,y > 0 and (x==0 or Y/X < y/x). It is possible to guard against
// a panic by ensuring the sqrt argument is positive.
func CalculateNativeSwapAmountAsymmetricRat(Y, X, y, x, f, r *big.Rat) big.Rat {
	var a_, b_, c_, d_, e_, f_, g_, h_, i_, j_, k_, l_, m_, n_, o_, p_, q_, r_, s_, t_, u_, v_, w_, x_, y_, z_, aa_, ab_, two, four big.Rat
	two.SetInt64(2)
	four.SetInt64(4)

	a_.Mul(f, r).Mul(&a_, X).Mul(&a_, y) // a_ := f * r * X * y
	b_.Mul(f, r).Mul(&b_, X).Mul(&b_, Y) // b_ := f * r * X * Y
	c_.Mul(f, X).Mul(&c_, y)             // c_ := f * X * y
	d_.Mul(f, X).Mul(&d_, Y)             // d_ := f * X * Y
	e_.Mul(r, X).Mul(&e_, y)             // e_ := r * X * y
	f_.Mul(r, X).Mul(&f_, Y)             // f_ := r * X * Y
	g_.Mul(&two, x).Mul(&g_, Y)          // g_ := 2 * x * Y
	h_.Mul(&two, X).Mul(&h_, Y)          // h_ := 2 * X * Y
	i_.Add(x, X)                         // i_ := x + X
	j_.Mul(x, Y).Mul(&j_, Y)             // j_ := x * Y * Y
	k_.Mul(X, y).Mul(&k_, Y)             // k_ := X * y * Y
	l_.Sub(&j_, &k_)                     // l_ := j_ - k_
	m_.Mul(&four, &i_).Mul(&m_, &l_)     // m_ := 4 * i_ * l_
	n_.Mul(f, r).Mul(&n_, X).Mul(&n_, y) // n_ := f * r * X * y
	o_.Mul(f, r).Mul(&o_, X).Mul(&o_, Y) // o_ := f * r * X * Y
	p_.Mul(f, X).Mul(&p_, y)             // p_ := f * X * y
	q_.Mul(f, X).Mul(&q_, Y)             // q_ := f * X * Y
	r_.Mul(r, X).Mul(&r_, y)             // r_ := r * X * y
	s_.Mul(r, X).Mul(&s_, Y)             // s_ := r * X * Y
	t_.Mul(&two, x).Mul(&t_, Y)          // t_ := 2 * x * Y
	u_.Mul(&two, X).Mul(&u_, Y)          // u_ := 2 * X * Y
	v_.Add(x, X).Mul(&v_, &two)          // v_ := 2 * (x + X)

	w_.Add(&e_, &f_).Add(&w_, &g_).Add(&w_, &h_).Sub(&w_, &a_).Sub(&w_, &b_).Sub(&w_, &c_).Sub(&w_, &d_) // w_ := e_ + f_ + g_ + h_ -a_ - b_ - c_ - d_  // w_ := -a_ - b_ - c_ - d_ + e_ + f_ + g_ + h_

	x_.Mul(&w_, &w_) // x_ := math.Pow(w_, 2)
	y_.Sub(&x_, &m_) // y_ := x_ - m_

	z_.SetInt(ApproxRatSquareRoot(&y_)) // z_ := math.Sqrt(y_)

	aa_.Add(&z_, &n_).Add(&aa_, &o_).Add(&aa_, &p_).Add(&aa_, &q_).Sub(&aa_, &r_).Sub(&aa_, &s_).Sub(&aa_, &t_).Sub(&aa_, &u_) // aa_ := z_ + n_ + o_ + p_ + q_ - r_ - s_ - t_ - u_

	ab_.Quo(&aa_, &v_) // ab_ := aa_ / v_

	return *ab_.Abs(&ab_)
}
