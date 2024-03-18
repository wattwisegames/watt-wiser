package main

import (
	"image/color"
	"math"

	"git.sr.ht/~whereswaldon/watt-wiser/f32color"
)

// relationColor := f32color.HSLA(float32(math.Mod(float64((i+1)*(off+1))*math.Phi, 1)), 0.9, 0.8, alpha)
var colors = func() []color.NRGBA {
	const target = 20
	out := []color.NRGBA{}
	for i := 0; i < target; i++ {
		oklch := f32color.Oklch{
			L: .5,
			C: .2,
			H: float32(math.Mod(float64(i+1)*math.Phi*2*math.Pi, 1)) * 360,
			A: 1,
		}
		out = append(out, oklch.NRGBA())
	}
	return out
}()
