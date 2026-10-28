package tui

import (
	"bytes"
	"encoding/binary"
)

// This is the small binary manifest fixture builder used by the APK package tests.
const (
	chunkXML        = 0x0003
	chunkStringPool = 0x0001
	chunkResourceID = 0x0180
	chunkStartElem  = 0x0102
	flagUTF8        = 1 << 8
	typeString      = 0x03
	typeIntDec      = 0x10
	resVersionCode  = 0x0101021b
	resVersionName  = 0x0101021c
	resMinSDK       = 0x0101020c
	resTargetSDK    = 0x01010270
)

func jobTestAXML(pkg, verName string, verCode int32, minSDK, targetSDK int32) []byte {
	stringsList := []string{
		"manifest",         // 0
		"package",          // 1
		"versionCode",      // 2
		"versionName",      // 3
		"uses-sdk",         // 4
		"minSdkVersion",    // 5
		"targetSdkVersion", // 6
		pkg,                // 7
		verName,            // 8
	}
	resIDs := []uint32{
		0,              // manifest
		0,              // package (no android: resource ID)
		resVersionCode, // versionCode
		resVersionName, // versionName
		0,              // uses-sdk
		resMinSDK,      // minSdkVersion
		resTargetSDK,   // targetSdkVersion
	}

	// String pool chunk (UTF-8)
	var strData bytes.Buffer
	offsets := make([]uint32, len(stringsList))
	for i, s := range stringsList {
		offsets[i] = uint32(strData.Len())
		b := []byte(s)
		strData.WriteByte(byte(len(b))) // char count (<128)
		strData.WriteByte(byte(len(b))) // byte count (<128)
		strData.Write(b)
		strData.WriteByte(0)
	}
	for strData.Len()%4 != 0 {
		strData.WriteByte(0)
	}

	spHeaderSize := uint16(28)
	stringsStart := uint32(spHeaderSize) + uint32(len(stringsList)*4)
	spChunkSize := stringsStart + uint32(strData.Len())

	var sp bytes.Buffer
	_ = binary.Write(&sp, binary.LittleEndian, uint16(chunkStringPool))
	_ = binary.Write(&sp, binary.LittleEndian, spHeaderSize)
	_ = binary.Write(&sp, binary.LittleEndian, spChunkSize)
	_ = binary.Write(&sp, binary.LittleEndian, uint32(len(stringsList))) // stringCount
	_ = binary.Write(&sp, binary.LittleEndian, uint32(0))                // styleCount
	_ = binary.Write(&sp, binary.LittleEndian, uint32(flagUTF8))         // flags
	_ = binary.Write(&sp, binary.LittleEndian, stringsStart)
	_ = binary.Write(&sp, binary.LittleEndian, uint32(0)) // stylesStart
	for _, off := range offsets {
		_ = binary.Write(&sp, binary.LittleEndian, off)
	}
	sp.Write(strData.Bytes())

	// Resource ID chunk
	var resBuf bytes.Buffer
	_ = binary.Write(&resBuf, binary.LittleEndian, uint16(chunkResourceID))
	_ = binary.Write(&resBuf, binary.LittleEndian, uint16(8))
	_ = binary.Write(&resBuf, binary.LittleEndian, uint32(8+len(resIDs)*4))
	for _, id := range resIDs {
		_ = binary.Write(&resBuf, binary.LittleEndian, id)
	}

	type attr struct {
		nameIdx uint32
		rawVal  uint32
		vType   uint8
		vData   uint32
	}
	makeStartElem := func(elemIdx uint32, attrs []attr) []byte {
		var eb bytes.Buffer
		hsz := uint16(16)
		csz := uint32(36 + len(attrs)*20)
		_ = binary.Write(&eb, binary.LittleEndian, uint16(chunkStartElem))
		_ = binary.Write(&eb, binary.LittleEndian, hsz)
		_ = binary.Write(&eb, binary.LittleEndian, csz)
		_ = binary.Write(&eb, binary.LittleEndian, uint32(1)) // lineNumber
		_ = binary.Write(&eb, binary.LittleEndian, uint32(0xffffffff))
		// body (20 bytes before attributes)
		_ = binary.Write(&eb, binary.LittleEndian, uint32(0xffffffff)) // ns
		_ = binary.Write(&eb, binary.LittleEndian, elemIdx)            // name
		_ = binary.Write(&eb, binary.LittleEndian, uint16(20))         // attrStart
		_ = binary.Write(&eb, binary.LittleEndian, uint16(20))         // attrSize
		_ = binary.Write(&eb, binary.LittleEndian, uint16(len(attrs))) // attrCount
		_ = binary.Write(&eb, binary.LittleEndian, uint16(0))
		_ = binary.Write(&eb, binary.LittleEndian, uint16(0))
		_ = binary.Write(&eb, binary.LittleEndian, uint16(0))
		for _, a := range attrs {
			_ = binary.Write(&eb, binary.LittleEndian, uint32(0xffffffff)) // ns
			_ = binary.Write(&eb, binary.LittleEndian, a.nameIdx)
			_ = binary.Write(&eb, binary.LittleEndian, a.rawVal)
			_ = binary.Write(&eb, binary.LittleEndian, uint16(8)) // size
			eb.WriteByte(0)
			eb.WriteByte(a.vType)
			_ = binary.Write(&eb, binary.LittleEndian, a.vData)
		}
		return eb.Bytes()
	}

	manifestElem := makeStartElem(0, []attr{
		{nameIdx: 1, rawVal: 7, vType: typeString, vData: 7},
		{nameIdx: 2, rawVal: 0xffffffff, vType: typeIntDec, vData: uint32(verCode)},
		{nameIdx: 3, rawVal: 8, vType: typeString, vData: 8},
	})
	sdkElem := makeStartElem(4, []attr{
		{nameIdx: 5, rawVal: 0xffffffff, vType: typeIntDec, vData: uint32(minSDK)},
		{nameIdx: 6, rawVal: 0xffffffff, vType: typeIntDec, vData: uint32(targetSDK)},
	})

	totalSize := 8 + sp.Len() + resBuf.Len() + len(manifestElem) + len(sdkElem)
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint16(chunkXML))
	_ = binary.Write(&out, binary.LittleEndian, uint16(8))
	_ = binary.Write(&out, binary.LittleEndian, uint32(totalSize))
	out.Write(sp.Bytes())
	out.Write(resBuf.Bytes())
	out.Write(manifestElem)
	out.Write(sdkElem)
	return out.Bytes()
}
