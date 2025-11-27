// +build amd64

#include "textflag.h"

// NOTE: hasAVX2 is implemented in Go via golang.org/x/sys/cpu to avoid
// assembly-based CPUID handling and to leverage the runtime's detection.

// func canMergeCoarseBatchAVX(
//     g1MinX, g1MaxX, g1MinY, g1MaxY *[4]float64,
//     g2MinX, g2MaxX, g2MinY, g2MaxY *[4]float64,
//     threshold float64,
// ) uint8
//
// This function checks 4 block pairs simultaneously using AVX2.
// Returns bitmask where bit i=1 means pair i passes the coarse merge check.
//
// Coarse check logic (per pair):
//   pass = (g1MaxX + threshold >= g2MinX) &&
//          (g2MaxX + threshold >= g1MinX) &&
//          (g1MaxY + threshold >= g2MinY) &&
//          (g2MaxY + threshold >= g1MinY)
//
TEXT ·canMergeCoarseBatchAVX(SB), NOSPLIT, $0-73
    // Load pointers from stack
    MOVQ g1MinX+0(FP), R8
    MOVQ g1MaxX+8(FP), R9
    MOVQ g1MinY+16(FP), R10
    MOVQ g1MaxY+24(FP), R11
    MOVQ g2MinX+32(FP), R12
    MOVQ g2MaxX+40(FP), R13
    MOVQ g2MinY+48(FP), R14
    MOVQ g2MaxY+56(FP), R15

    // Broadcast threshold to all 4 lanes of YMM register
    VBROADCASTSD threshold+64(FP), Y0  // Y0 = [threshold, threshold, threshold, threshold]

    // Load g1MaxX and add threshold
    VMOVUPD (R9), Y1           // Y1 = g1MaxX[0:4]
    VADDPD Y0, Y1, Y1          // Y1 = g1MaxX + threshold

    // Load g2MinX and compare: g1MaxX + threshold >= g2MinX
    VMOVUPD (R12), Y2          // Y2 = g2MinX[0:4]
    VCMPPD $13, Y2, Y1, Y3     // Y3 = (Y1 >= Y2) ? 0xFFFFFFFF : 0  (predicate 13 = GE)

    // Load g2MaxX and add threshold
    VMOVUPD (R13), Y1          // Y1 = g2MaxX[0:4]
    VADDPD Y0, Y1, Y1          // Y1 = g2MaxX + threshold

    // Load g1MinX and compare: g2MaxX + threshold >= g1MinX
    VMOVUPD (R8), Y2           // Y2 = g1MinX[0:4]
    VCMPPD $13, Y2, Y1, Y4     // Y4 = (Y1 >= Y2) ? 0xFFFFFFFF : 0

    // AND the X-axis results
    VANDPD Y3, Y4, Y3          // Y3 = X-axis pass mask

    // Load g1MaxY and add threshold
    VMOVUPD (R11), Y1          // Y1 = g1MaxY[0:4]
    VADDPD Y0, Y1, Y1          // Y1 = g1MaxY + threshold

    // Load g2MinY and compare: g1MaxY + threshold >= g2MinY
    VMOVUPD (R14), Y2          // Y2 = g2MinY[0:4]
    VCMPPD $13, Y2, Y1, Y4     // Y4 = (Y1 >= Y2) ? 0xFFFFFFFF : 0

    // AND with X-axis result
    VANDPD Y3, Y4, Y3

    // Load g2MaxY and add threshold
    VMOVUPD (R15), Y1          // Y1 = g2MaxY[0:4]
    VADDPD Y0, Y1, Y1          // Y1 = g2MaxY + threshold

    // Load g1MinY and compare: g2MaxY + threshold >= g1MinY
    VMOVUPD (R10), Y2          // Y2 = g1MinY[0:4]
    VCMPPD $13, Y2, Y1, Y4     // Y4 = (Y1 >= Y2) ? 0xFFFFFFFF : 0

    // Final AND
    VANDPD Y3, Y4, Y3          // Y3 = final pass mask (4 x 64-bit)

    // Extract bitmask from Y3
    // VMOVMSKPD extracts the sign bits of each 64-bit element
    VMOVMSKPD Y3, AX           // AX = 4-bit mask

    // Store result
    MOVB AL, ret+72(FP)

    // Clear upper bits of YMM registers (AVX-SSE transition penalty)
    VZEROUPPER
    RET
