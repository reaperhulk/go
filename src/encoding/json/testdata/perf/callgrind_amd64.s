// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "textflag.h"

// Valgrind's documented amd64 client-request ABI: AX points to six words,
// DX holds the default result, and the rotate/xchg sequence marks a request.
TEXT ·callgrind(SB), NOSPLIT, $48-8
	MOVQ request+0(FP), AX
	MOVQ AX, 0(SP)
	MOVQ $0, 8(SP)
	MOVQ $0, 16(SP)
	MOVQ $0, 24(SP)
	MOVQ $0, 32(SP)
	MOVQ $0, 40(SP)
	LEAQ 0(SP), AX
	MOVQ $0, DX
	ROLQ $3, DI
	ROLQ $13, DI
	ROLQ $61, DI
	ROLQ $51, DI
	XCHGQ BX, BX
	RET
