package storage

import (
	"container/heap"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
)

func TextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func EncodeVector(vector []float32) []byte {
	raw := make([]byte, 4*len(vector))
	for i, v := range vector {
		binary.LittleEndian.PutUint32(raw[4*i:], math.Float32bits(v))
	}
	return raw
}

func DecodeVector(raw []byte) ([]float32, error) {
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("vector blob length %d is not a multiple of 4", len(raw))
	}
	vector := make([]float32, len(raw)/4)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return vector, nil
}

func CosineSimilarity(a []float32, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

type scored[T any] struct {
	item  T
	score float64
}

type minScoreHeap[T any] []scored[T]

func (h minScoreHeap[T]) Len() int             { return len(h) }
func (h minScoreHeap[T]) Less(i, j int) bool   { return h[i].score < h[j].score }
func (h minScoreHeap[T]) Swap(i, j int)        { h[i], h[j] = h[j], h[i] }
func (h *minScoreHeap[T]) Push(x any)          { *h = append(*h, x.(scored[T])) }
func (h *minScoreHeap[T]) Pop() any            { old := *h; x := old[len(old)-1]; *h = old[:len(old)-1]; return x }
func (h *minScoreHeap[T]) worstScore() float64 { return (*h)[0].score }

type TopK[T any] struct {
	limit int
	heap  minScoreHeap[T]
}

func NewTopK[T any](limit int) *TopK[T] {
	return &TopK[T]{limit: limit}
}

func (t *TopK[T]) Add(item T, score float64) {
	if t.limit <= 0 {
		return
	}
	if t.heap.Len() < t.limit {
		heap.Push(&t.heap, scored[T]{item: item, score: score})
		return
	}
	if score <= t.heap.worstScore() {
		return
	}
	t.heap[0] = scored[T]{item: item, score: score}
	heap.Fix(&t.heap, 0)
}

func (t *TopK[T]) SortedDescending() ([]T, []float64) {
	items := make([]T, t.heap.Len())
	scores := make([]float64, t.heap.Len())
	for i := t.heap.Len() - 1; i >= 0; i-- {
		popped := heap.Pop(&t.heap).(scored[T])
		items[i] = popped.item
		scores[i] = popped.score
	}
	return items, scores
}
