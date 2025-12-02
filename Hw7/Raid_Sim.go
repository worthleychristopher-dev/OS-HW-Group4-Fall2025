// raid_sim.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

const (
	NumDisks  = 5
	DefaultBS = 4 * 1024 // 4 KB block size
	TotalSize = 100 * 1024 * 1024 // 100 MB total workload
)

// RAID interface
type RAID interface {
	Write(blockNum int64, data []byte) error
	Read(blockNum int64) ([]byte, error)
	Close() error
	EffectiveCapacityPerDisk() int // in number of data disks (for calculation)
}

// helper to open or create disk files
func openDisks(baseDir string) ([]*os.File, error) {
	files := make([]*os.File, NumDisks)
	for i := 0; i < NumDisks; i++ {
		name := filepath.Join(baseDir, fmt.Sprintf("disk%d.dat", i))
		f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			// close any already opened
			for j := 0; j < i; j++ {
				files[j].Close()
			}
			return nil, err
		}
		files[i] = f
	}
	return files, nil
}

// Common helper: ensure len(data) == blockSize
func checkBlockSize(data []byte, blockSize int) error {
	if len(data) != blockSize {
		return fmt.Errorf("data length %d must equal block size %d", len(data), blockSize)
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// RAID 0
////////////////////////////////////////////////////////////////////////////////
type RAID0 struct {
	disks     []*os.File
	blockSize int
}

func NewRAID0(disks []*os.File, blockSize int) *RAID0 {
	return &RAID0{disks: disks, blockSize: blockSize}
}

func (r *RAID0) Write(blockNum int64, data []byte) error {
	if err := checkBlockSize(data, r.blockSize); err != nil {
		return err
	}
	disk := int(blockNum % NumDisks)
	stripe := blockNum / NumDisks
	offset := stripe * int64(r.blockSize)
	_, err := r.disks[disk].WriteAt(data, offset)
	if err != nil {
		return err
	}
	return r.disks[disk].Sync()
}

func (r *RAID0) Read(blockNum int64) ([]byte, error) {
	disk := int(blockNum % NumDisks)
	stripe := blockNum / NumDisks
	offset := stripe * int64(r.blockSize)
	buf := make([]byte, r.blockSize)
	_, err := io.ReadFull(io.NewSectionReader(r.disks[disk], offset, int64(r.blockSize)), buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (r *RAID0) Close() error {
	for _, f := range r.disks {
		f.Close()
	}
	return nil
}

func (r *RAID0) EffectiveCapacityPerDisk() int { return NumDisks } // RAID0: effective = N * disk

////////////////////////////////////////////////////////////////////////////////
// RAID 1 (mirror across all disks)
////////////////////////////////////////////////////////////////////////////////
type RAID1 struct {
	disks     []*os.File
	blockSize int
}

func NewRAID1(disks []*os.File, blockSize int) *RAID1 {
	return &RAID1{disks: disks, blockSize: blockSize}
}

func (r *RAID1) Write(blockNum int64, data []byte) error {
	if err := checkBlockSize(data, r.blockSize); err != nil {
		return err
	}
	offset := blockNum * int64(r.blockSize)
	for _, d := range r.disks {
		_, err := d.WriteAt(data, offset)
		if err != nil {
			return err
		}
		// fsync each disk to simulate write durability
		if err := d.Sync(); err != nil {
			return err
		}
	}
	return nil
}

func (r *RAID1) Read(blockNum int64) ([]byte, error) {
	offset := blockNum * int64(r.blockSize)
	buf := make([]byte, r.blockSize)
	_, err := io.ReadFull(io.NewSectionReader(r.disks[0], offset, int64(r.blockSize)), buf)
	if err != nil {
		// try other disks if first fails
		for i := 1; i < len(r.disks); i++ {
			_, err2 := io.ReadFull(io.NewSectionReader(r.disks[i], offset, int64(r.blockSize)), buf)
			if err2 == nil {
				return buf, nil
			}
		}
		return nil, err
	}
	return buf, nil
}

func (r *RAID1) Close() error {
	for _, f := range r.disks {
		f.Close()
	}
	return nil
}

func (r *RAID1) EffectiveCapacityPerDisk() int { return 1 } // only one disk of N capacity effectively

////////////////////////////////////////////////////////////////////////////////
// RAID 4 (dedicated parity on disk 4)
////////////////////////////////////////////////////////////////////////////////
type RAID4 struct {
	disks     []*os.File
	blockSize int
	parityDisk int
	dataDisks int
}

func NewRAID4(disks []*os.File, blockSize int) *RAID4 {
	return &RAID4{
		disks: disks, blockSize: blockSize,
		parityDisk: NumDisks-1,
		dataDisks: NumDisks-1,
	}
}

func xorBytes(dst, a, b []byte) {
	// dst = a XOR b (dst may be same as a)
	for i := range a {
		dst[i] = a[i] ^ b[i]
	}
}

func xorBytesMany(parts [][]byte, out []byte) {
	// XOR all into out
	for i := range out {
		out[i] = 0
	}
	for _, p := range parts {
		for i := range out {
			out[i] ^= p[i]
		}
	}
}

func (r *RAID4) Write(blockNum int64, data []byte) error {
	if err := checkBlockSize(data, r.blockSize); err != nil {
		return err
	}
	// Determine stripe and disk within stripe
	stripe := blockNum / int64(r.dataDisks)
	didx := int(blockNum % int64(r.dataDisks)) // data disk index in [0..dataDisks-1]
	physDisk := didx
	offset := stripe * int64(r.blockSize)

	// read current data blocks in stripe (to compute parity)
	parts := make([][]byte, r.dataDisks)
	for i := 0; i < r.dataDisks; i++ {
		buf := make([]byte, r.blockSize)
		_, err := io.ReadFull(io.NewSectionReader(r.disks[i], offset, int64(r.blockSize)), buf)
		if err != nil {
			// treat missing/uninitialized as zero (works for simulation)
			for j := range buf {
				buf[j] = 0
			}
		}
		parts[i] = buf
	}
	// replace the changed data part
	parts[didx] = append([]byte(nil), data...) // copy

	// compute parity
	parity := make([]byte, r.blockSize)
	xorBytesMany(parts, parity)

	// write data to physDisk
	if _, err := r.disks[physDisk].WriteAt(data, offset); err != nil {
		return err
	}
	if err := r.disks[physDisk].Sync(); err != nil {
		return err
	}

	// write parity to parity disk
	if _, err := r.disks[r.parityDisk].WriteAt(parity, offset); err != nil {
		return err
	}
	return r.disks[r.parityDisk].Sync()
}

func (r *RAID4) Read(blockNum int64) ([]byte, error) {
	stripe := blockNum / int64(r.dataDisks)
	didx := int(blockNum % int64(r.dataDisks))
	offset := stripe * int64(r.blockSize)

	buf := make([]byte, r.blockSize)
	_, err := io.ReadFull(io.NewSectionReader(r.disks[didx], offset, int64(r.blockSize)), buf)
	if err == nil {
		return buf, nil
	}
	// If read fails, reconstruct using parity and other data disks (not expected in this sim)
	parts := make([][]byte, r.dataDisks)
	for i := 0; i < r.dataDisks; i++ {
		b := make([]byte, r.blockSize)
		if i == didx { // missing part
			for j := range b {
				b[j] = 0
			}
		} else {
			_, err2 := io.ReadFull(io.NewSectionReader(r.disks[i], offset, int64(r.blockSize)), b)
			if err2 != nil {
				for j := range b {
					b[j] = 0
				}
			}
		}
		parts[i] = b
	}
	parity := make([]byte, r.blockSize)
	_, _ = io.ReadFull(io.NewSectionReader(r.disks[r.parityDisk], offset, int64(r.blockSize)), parity)
	// reconstruct missing = XOR(all parts except missing) XOR parity
	for i := 0; i < r.blockSize; i++ {
		val := parity[i]
		for j := 0; j < r.dataDisks; j++ {
			if j == didx {
				continue
			}
			val ^= parts[j][i]
		}
		buf[i] = val
	}
	return buf, nil
}

func (r *RAID4) Close() error {
	for _, f := range r.disks {
		f.Close()
	}
	return nil
}

func (r *RAID4) EffectiveCapacityPerDisk() int { return r.dataDisks }

////////////////////////////////////////////////////////////////////////////////
// RAID 5 (distributed rotating parity)
////////////////////////////////////////////////////////////////////////////////
type RAID5 struct {
	disks     []*os.File
	blockSize int
	nDisks int
	dataDisks int
}

func NewRAID5(disks []*os.File, blockSize int) *RAID5 {
	return &RAID5{disks: disks, blockSize: blockSize, nDisks: len(disks), dataDisks: len(disks)-1}
}

func (r *RAID5) stripeParityIndex(stripe int64) int {
	return int(stripe % int64(r.nDisks))
}

func (r *RAID5) physicalDiskForData(stripe int64, dataIndex int) int {
	// The parity disk takes one position; dataIndex maps to physical skipping parity
	par := r.stripeParityIndex(stripe)
	phys := dataIndex
	if phys >= par {
		phys++
	}
	return phys
}

func (r *RAID5) Write(blockNum int64, data []byte) error {
	if err := checkBlockSize(data, r.blockSize); err != nil {
		return err
	}
	stripe := blockNum / int64(r.dataDisks)
	dataIndex := int(blockNum % int64(r.dataDisks))
	par := r.stripeParityIndex(stripe)
	phys := r.physicalDiskForData(stripe, dataIndex)
	offset := stripe * int64(r.blockSize)

	// read all data parts (data disks only) reconstruct array aligned with physical disks except parity
	parts := make([][]byte, r.nDisks)
	for i := 0; i < r.nDisks; i++ {
		parts[i] = make([]byte, r.blockSize)
		// skip parity disk (we still read it)
		_, err := io.ReadFull(io.NewSectionReader(r.disks[i], offset, int64(r.blockSize)), parts[i])
		if err != nil {
			for j := range parts[i] {
				parts[i][j] = 0
			}
		}
	}

	// set new data into phys slot
	parts[phys] = append([]byte(nil), data...)

	// compute parity across all disks (XOR of all physical positions)
	parity := make([]byte, r.blockSize)
	for i := 0; i < r.blockSize; i++ {
		val := byte(0)
		for d := 0; d < r.nDisks; d++ {
			// parity is XOR of data blocks excluding the parity slot? In RAID5 parity is XOR of all data positions (parity slot holds parity).
			// We compute XOR across all non-parity disks (data positions)
			if d == par {
				continue
			}
			val ^= parts[d][i]
		}
		parity[i] = val
	}

	// write data to phys
	if _, err := r.disks[phys].WriteAt(data, offset); err != nil {
		return err
	}
	if err := r.disks[phys].Sync(); err != nil {
		return err
	}

	// write parity to parity slot
	if _, err := r.disks[par].WriteAt(parity, offset); err != nil {
		return err
	}
	return r.disks[par].Sync()
}

func (r *RAID5) Read(blockNum int64) ([]byte, error) {
	stripe := blockNum / int64(r.dataDisks)
	dataIndex := int(blockNum % int64(r.dataDisks))
	par := r.stripeParityIndex(stripe)
	phys := r.physicalDiskForData(stripe, dataIndex)
	offset := stripe * int64(r.blockSize)

	buf := make([]byte, r.blockSize)
	_, err := io.ReadFull(io.NewSectionReader(r.disks[phys], offset, int64(r.blockSize)), buf)
	if err == nil {
		return buf, nil
	}
	// reconstruct using parity and other disks (not expected in simulation)
	parts := make([][]byte, r.nDisks)
	for i := 0; i < r.nDisks; i++ {
		b := make([]byte, r.blockSize)
		if i == phys {
			for j := range b {
				b[j] = 0
			}
		} else {
			_, err2 := io.ReadFull(io.NewSectionReader(r.disks[i], offset, int64(r.blockSize)), b)
			if err2 != nil {
				for j := range b {
					b[j] = 0
				}
			}
		}
		parts[i] = b
	}
	// parity = XOR of data positions; reconstruct missing by XOR of present data and parity
	for i := 0; i < r.blockSize; i++ {
		val := byte(0)
		for d := 0; d < r.nDisks; d++ {
			if d == phys {
				continue
			}
			if d == par {
				val ^= parts[d][i] // parity slot
			} else {
				val ^= parts[d][i]
			}
		}
		// val now equals missing data
		buf[i] = val
	}
	return buf, nil
}

func (r *RAID5) Close() error {
	for _, f := range r.disks {
		f.Close()
	}
	return nil
}

func (r *RAID5) EffectiveCapacityPerDisk() int { return r.dataDisks }

////////////////////////////////////////////////////////////////////////////////
// Benchmarking harness
////////////////////////////////////////////////////////////////////////////////

type BenchResult struct {
	Name                 string
	TotalBlocks          int64
	BlockSize            int
	WriteDuration        time.Duration
	ReadDuration         time.Duration
	WritePerBlock        time.Duration
	ReadPerBlock         time.Duration
	EffectiveCapacityPct float64 // logical storage / raw sum
}

func runBenchmark(name string, ra RAID, blockSize int, totalBytes int64) (BenchResult, error) {
	totalBlocks := totalBytes / int64(blockSize)
	if totalBlocks == 0 {
		return BenchResult{}, fmt.Errorf("totalBytes too small for given blockSize")
	}

	// generate deterministic data pattern per block so we can verify read correctness
	// pattern: blockNum (int64) followed by PRNG-mix to fill block
	makeBlock := func(bnum int64) []byte {
		buf := make([]byte, blockSize)
		// first 8 bytes: block number (little endian)
		binary.LittleEndian.PutUint64(buf[:8], uint64(bnum))
		// fill rest with deterministic pseudorandom seeded by block number
		rnd := rand.New(rand.NewSource(bnum))
		fill := make([]byte, blockSize-8)
		_, _ = rnd.Read(fill)
		copy(buf[8:], fill)
		return buf
	}

	// Write phase
	startW := time.Now()
	for i := int64(0); i < totalBlocks; i++ {
		block := makeBlock(i)
		if err := ra.Write(i, block); err != nil {
			return BenchResult{}, fmt.Errorf("write failed at block %d: %v", i, err)
		}
	}
	writeDur := time.Since(startW)

	// Read & verify phase
	startR := time.Now()
	for i := int64(0); i < totalBlocks; i++ {
		expected := makeBlock(i)
		got, err := ra.Read(i)
		if err != nil {
			return BenchResult{}, fmt.Errorf("read failed at block %d: %v", i, err)
		}
		if !bytes.Equal(expected, got) {
			// attempt to show first differing byte index
			idx := -1
			minL := len(expected)
			if len(got) < minL {
				minL = len(got)
			}
			for j := 0; j < minL; j++ {
				if expected[j] != got[j] {
					idx = j
					break
				}
			}
			return BenchResult{}, fmt.Errorf("data mismatch at block %d (first diff byte %d)", i, idx)
		}
	}
	readDur := time.Since(startR)

	// compute per-block times
	res := BenchResult{
		Name:                 name,
		TotalBlocks:          totalBlocks,
		BlockSize:            blockSize,
		WriteDuration:        writeDur,
		ReadDuration:         readDur,
		WritePerBlock:        time.Duration(int64(writeDur) / totalBlocks),
		ReadPerBlock:         time.Duration(int64(readDur) / totalBlocks),
	}
	return res, nil
}

func printSummary(res BenchResult, nDisks int) {
	rawCapacity := int64(nDisks) * int64(res.TotalBlocks) * int64(res.BlockSize) // bytes if all disks used for data
	logicalCapacity := int64(res.TotalBlocks) * int64(res.BlockSize) // by our mapping of logical blocks
	// In this simulation, logicalCapacity is what we wrote.
	fmt.Println("---- Benchmark summary ----")
	fmt.Printf("RAID level: %s\n", res.Name)
	fmt.Printf("Block size: %d bytes\n", res.BlockSize)
	fmt.Printf("Total blocks written/read: %d (total data size %.2f MB)\n", res.TotalBlocks, float64(res.TotalBlocks*int64(res.BlockSize))/1024.0/1024.0)
	fmt.Printf("Write: total %v, per-block %v\n", res.WriteDuration, res.WritePerBlock)
	fmt.Printf("Read:  total %v, per-block %v\n", res.ReadDuration, res.ReadPerBlock)
	// effective storage (for visualization)
	fmt.Printf("Effective logical storage used: %.2f MB\n", float64(logicalCapacity)/1024.0/1024.0)
	fmt.Printf("Raw disks combined size (if counted): %.2f MB\n", float64(rawCapacity)/1024.0/1024.0)
	fmt.Println("---------------------------")
}

func truncateDisks(disks []*os.File, size int64) error {
	for _, f := range disks {
		if err := f.Truncate(size); err != nil {
			return err
		}
	}
	return nil
}

func cleanupFiles(baseDir string) {
	for i := 0; i < NumDisks; i++ {
		_ = os.Remove(filepath.Join(baseDir, fmt.Sprintf("disk%d.dat", i)))
	}
}

func main() {
	baseDir := "." // current directory; change if you want
	blockSize := DefaultBS
	totalBytes := TotalSize

	fmt.Printf("Simulating RAID levels with %d disk files in %s\nBlock size: %d bytes\nTotal workload: %.2f MB\n\n",
		NumDisks, baseDir, blockSize, float64(totalBytes)/1024.0/1024.0)

	// ensure clean state by removing existing disk files (optional)
	cleanupFiles(baseDir)

	// open disks
	disks, err := openDisks(baseDir)
	if err != nil {
		fmt.Println("failed to open disk files:", err)
		return
	}
	// pre-allocate disk file sizes to totalBytes per-disk for stable benchmarks (optional)
	// here we give each disk enough space; a single disk capacity equals totalBytes (conservative)
	if err := truncateDisks(disks, int64(totalBytes)); err != nil {
		fmt.Println("truncate failed:", err)
		// continue anyway
	}

	// instantiate RAID levels
	//r0 := NewRAID0(disks, blockSize)
	//BenchmarkRAID("RAID0", r0)

	//r1 := NewRAID1(disks, blockSize)
    //BenchmarkRAID("RAID1", r1)
	//r4 := NewRAID4(disks, blockSize)
	//BenchmarkRAID("RAID4", r4)

	//r5 := NewRAID5(disks, blockSize)
	//BenchmarkRAID("RAID5", r5)

	// We'll run each RAID in sequence. To ensure same initial conditions, we will (for simplicity)
	// close and re-open disk files between runs and clear them.
	// For a real detailed benchmark you'd create separate disk files per RAID, but to keep this single-file
	// simulation simple we reinitialize contents between runs.

	var results []BenchResult

	// helper to run one RAID instance with fresh disks
	runOnce := func(name string, constructor func([]*os.File) RAID) {
		// close current files and re-open new ones (to wipe)
		for _, f := range disks {
			f.Close()
		}
		// recreate files empty
		cleanupFiles(baseDir)
		var err error
		disks, err = openDisks(baseDir)
		if err != nil {
			fmt.Println("failed to open disks:", err)
			return
		}
		_ = truncateDisks(disks, int64(totalBytes)) // give each disk space

		raid := constructor(disks)
		fmt.Printf("Running benchmark: %s ...\n", name)
		res, err := runBenchmark(name, raid, blockSize, int64(totalBytes))
		if err != nil {
			fmt.Printf("Benchmark %s failed: %v\n", name, err)
			raid.Close()
			return
		}
		results = append(results, res)
		printSummary(res, NumDisks)
		raid.Close()
	}

	runOnce("RAID0", func(d []*os.File) RAID { return NewRAID0(d, blockSize) })
	runOnce("RAID1", func(d []*os.File) RAID { return NewRAID1(d, blockSize) })
	runOnce("RAID4", func(d []*os.File) RAID { return NewRAID4(d, blockSize) })
	runOnce("RAID5", func(d []*os.File) RAID { return NewRAID5(d, blockSize) })

	// Final high-level comparison table print
	fmt.Println("\n=== Summary comparison (high-level) ===")
	fmt.Printf("%-8s %-12s %-12s %-12s\n", "RAID", "Write(s)", "Read(s)", "MB written")
	for _, r := range results {
		fmt.Printf("%-8s %-12.3f %-12.3f %8.2f\n",
			r.Name, r.WriteDuration.Seconds(), r.ReadDuration.Seconds(),
			float64(r.TotalBlocks*int64(r.BlockSize))/1024.0/1024.0)
	}
	fmt.Println("=======================================")

	fmt.Println("\nNote on effective storage capacity (for N=5):")
	fmt.Println("RAID0: effective capacity (N * S)	= 500")
	fmt.Println("RAID1: effective capacity (S)		= 100 (mirrored to all disks)")
	fmt.Println("RAID4: effective capacity ([N-1] * S)	= 400 (1 disk dedicated parity)")
	fmt.Println("RAID5: effective capacity ([N-1] * S)	= 400 (distributed parity)")
}
