package data

import (
    "math/rand"
    "strings"
    "time"
)

var (
    letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
    alnumSpace = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ")
    continents = []string{"North America", "Asia", "South America", "Europe", "Africa", "Australia"}
)

// init seeds the RNG; callers can override for reproducibility if needed.
func init() {
    rand.Seed(time.Now().UnixNano())
}

// GenerateRandomRecord returns a CSV record as []byte: id,name,address,continent
// Optimized to minimize allocations by using a strings.Builder with preallocation.
func GenerateRandomRecord() []byte {
    // id
    id := rand.Int31()

    // name 10-15 letters
    nameLen := 10 + rand.Intn(6)
    var nameBuilder strings.Builder
    nameBuilder.Grow(nameLen)
    for i := 0; i < nameLen; i++ {
        nameBuilder.WriteRune(letters[rand.Intn(len(letters))])
    }

    // address 15-20 alnum+space
    addrLen := 15 + rand.Intn(6)
    var addrBuilder strings.Builder
    addrBuilder.Grow(addrLen)
    for i := 0; i < addrLen; i++ {
        addrBuilder.WriteRune(alnumSpace[rand.Intn(len(alnumSpace))])
    }

    continent := continents[rand.Intn(len(continents))]

    // CSV: id,name,address,continent
    // Estimate: id up to 10 chars + commas + name + address + continent
    var b strings.Builder
    b.Grow(10 + 1 + nameLen + 1 + addrLen + 1 + len(continent))
    // Write int32 without fmt to avoid allocations
    writeInt32(&b, id)
    b.WriteByte(',')
    b.WriteString(nameBuilder.String())
    b.WriteByte(',')
    b.WriteString(addrBuilder.String())
    b.WriteByte(',')
    b.WriteString(continent)

    return []byte(b.String())
}

func writeInt32(b *strings.Builder, v int32) {
    // Convert positive int32 to decimal without fmt
    if v == 0 {
        b.WriteByte('0')
        return
    }
    // handle negative
    if v < 0 {
        b.WriteByte('-')
        v = -v
    }
    var buf [11]byte // enough for -2147483648
    i := len(buf)
    for v > 0 {
        i--
        buf[i] = byte('0' + (v % 10))
        v /= 10
    }
    b.Write(buf[i:])
}


