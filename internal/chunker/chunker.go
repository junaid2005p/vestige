// Package chunker splits byte streams at content-derived boundaries.
package chunker

import (
	"fmt"
	"io"
	"math/bits"
)

const Algorithm = "buzhash64-v1"

// Config defines chunk-size bounds. TargetSize must be a power of two.
type Config struct {
	Window     int
	MinSize    int
	TargetSize int
	MaxSize    int
}

func DefaultConfig() Config {
	return Config{Window: 64, MinSize: 512 * 1024, TargetSize: 2 * 1024 * 1024, MaxSize: 8 * 1024 * 1024}
}

func (c Config) Validate() error {
	if c.Window < 1 || c.MinSize < 1 || c.MinSize > c.TargetSize || c.TargetSize > c.MaxSize {
		return fmt.Errorf("invalid chunking sizes: window=%d min=%d target=%d max=%d", c.Window, c.MinSize, c.TargetSize, c.MaxSize)
	}
	if c.TargetSize&(c.TargetSize-1) != 0 {
		return fmt.Errorf("target chunk size must be a power of two")
	}
	return nil
}

// Split reads r and calls emit once for every non-empty chunk. Chunks are
// bounded in memory by MaxSize plus the read buffer.
func Split(r io.Reader, c Config, emit func([]byte) error) error {
	if err := c.Validate(); err != nil {
		return err
	}
	buf := make([]byte, 0, c.MaxSize)
	readBuf := make([]byte, 128*1024)
	window := make([]byte, c.Window)
	var fingerprint uint64
	seen := 0
	pos := 0
	mask := uint64(c.TargetSize - 1)

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if err := emit(buf); err != nil {
			return err
		}
		buf = make([]byte, 0, c.MaxSize)
		return nil
	}

	for {
		n, err := r.Read(readBuf)
		for _, b := range readBuf[:n] {
			buf = append(buf, b)
			if seen < c.Window {
				window[seen] = b
				seen++
				fingerprint = bits.RotateLeft64(fingerprint, 1) ^ table[b]
			} else {
				out := window[pos]
				window[pos] = b
				pos = (pos + 1) % c.Window
				fingerprint = bits.RotateLeft64(fingerprint, 1) ^ table[b] ^ bits.RotateLeft64(table[out], c.Window%64)
			}
			if len(buf) >= c.MaxSize || (len(buf) >= c.MinSize && seen >= c.Window && fingerprint&mask == 0) {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		if err == io.EOF {
			return flush()
		}
		if err != nil {
			return err
		}
	}
}

// Fixed deterministic random-looking values used by buzhash64-v1.
var table = [...]uint64{
	0x4f1bbcdc6763c059, 0x0e2243d913ca52ad, 0xbffea44754096380, 0x5175202db3d7a67d,
	0x9dfb4c7cde2e7961, 0xae1642933f0b4197, 0xc402e1ee8911d0cc, 0x24973bca45280acb,
	0x394c6ccf342f40a7, 0xa09d6a3d3dc3917a, 0xcff30c5a9cf90bb0, 0x8407c4715aeac45e,
	0x23d6a22e7f143388, 0x56c7036c3e3bd33c, 0xd1c42a0fa0ee9c86, 0xb458ef3f3f9579b8,
	0x6d3dc52c69a4b8a3, 0x8ea9c54c55f668c1, 0x9b857fb760251c8b, 0xf2ad6d2fda52c33d,
	0x9a6f831b2ac5f0e6, 0xda9d1e7a3cc39fc3, 0x3d4b8d0491e943c9, 0x8cf5b95bd2603e2f,
	0x63033b0ca389c35a, 0x456e4d7574ad70c0, 0x951c604af4649bf7, 0x9b26f4ff37f3bb77,
	0xb5a28b3071f3c9d9, 0x5b5f7d8599e24118, 0x76c9600b7ceda0a0, 0x67a7c7ee45c3a7d8,
	0x522c049c8178d702, 0x68c62a1e344e78e1, 0x06b39596556a3a85, 0x50e0cf70d9dc3230,
	0x89a4f9fe54cc7437, 0x7a08a12f679df2b6, 0x68b896ebc4a5f032, 0xb7e6de94c6e5f888,
	0x33b47ab3354ab0a9, 0xc6f0575469a8c4f2, 0x2d3d92e5dbba0b51, 0x14aa02c05b4a505e,
	0x1a61856721f68bcc, 0x5777ab92b8b5cb7d, 0x6a1bd5b9f696ddd4, 0xcb19529de8b7367c,
	0x08ae93ca8a77f671, 0xdb825d19a52654f8, 0x40e1575cc7d7097b, 0x1bba1b5f35e75569,
	0x7441a5a5cf3871eb, 0x76cb07b53ad2619f, 0xdb04854ce932b56e, 0xb1eec51b154c0c6c,
	0x0ec1c4764c2ae671, 0x25ad6a3c4e498514, 0x2ef67a18575fd96b, 0x9c066d6b461db438,
	0x7dd5e5b1c9c6608c, 0xac12105c46f0e1c8, 0xa1b7d4788e3536e4, 0x942058b9c448f3d6,
	0xdc383759ece8310c, 0xb741661294747ebd, 0x973d56f17e94f3ad, 0x9a7c416f9d291cfc,
	0x4f3360b850fb3ef7, 0x70c97b67bbd2ca6f, 0x9f468b3a67f9a877, 0x76b75f9325a901bb,
	0x4f4578b54b4850ad, 0x0784c7e9c193e7a5, 0x56b3a9e77f371133, 0x8b42ae08c6c4b884,
	0x00f7f65e603d0e79, 0x071c7bb879e2a062, 0xe9e0837e74b9c405, 0x5a18d59641cf0cb3,
	0x818cdd2a800e29cd, 0x39d9b4c3e5372c3b, 0x4a5ff6fd836c9b44, 0x2575250c2f2869f3,
	0x0f6d333567ea2ea4, 0x7718985766c1ebc3, 0xaf2d2e6f3a3c07a1, 0x230e5d5400c36cf1,
	0x2ee84bdf1f8d6c14, 0x96745ad9ec935ac5, 0x3db20ce8835f2e86, 0xf6bc7b3190c604f9,
	0x47bfc87fc74aa0a5, 0x1cf3c8ed2d4636ad, 0x75df4fae30f7118d, 0xbe19e2ffde84a72e,
	0x1a4570155cb25a16, 0xb5f38d2af4d7c6ba, 0x091d1b5b0ca57e5f, 0xb3f3c6c561bce0e8,
	0x716bae1588e1b3ec, 0xa0dbb53044610856, 0xbb84af8ae08870a8, 0x3d244271b5bd62ce,
	0x8c5355eac1775f10, 0x975f0f2957b76730, 0x7b45d9d06f4050a7, 0xa5d6d0a3c42fba64,
	0xe16608090485c927, 0x4b1040dafd2f6ea3, 0x1b2b01a2cdd3b74d, 0x805b639054b021c3,
	0x34b76f90255eb73f, 0x8c417f9c0be5cfe8, 0x1fc05eb077434350, 0x0a6fd4d8b0c3521c,
	0x10f8bb8c7f5c3a4d, 0xf68120e9a0d85f6e, 0x6a30f38bb2bdb9d4, 0x5aa21d886610d5a4,
	0x8c18b439a5c4ad68, 0xf00f46c4b968b443, 0x2c96472b391e8e04, 0x4c6cde7c0a90f5e2,
	0x1a01528a9e4a0587, 0xb77dbcc9bd806f5a, 0x1d8b23300ba32058, 0x9636c5e6e2a9b84a,
	0x1051d4058b0a39d9, 0x334131abea0b0de1, 0x2de1c0c3a0265aa8, 0x6fd9dbf20348b7f9,
	0x49acdc2e290467cb, 0x63b7c314326ed700, 0x057ca3ac1fa6ba94, 0x0d043fe66f3d7ed5,
	0xb1843b300251ae79, 0x38e182942963a74e, 0xc0a48dc5eb8a5721, 0xe04cb74e2e4acf81,
	0xb5ce46eb5a2600d0, 0xddbadcead2e2a22e, 0x9ecf9658cb719d98, 0x50d4b7857eb2fa94,
	0x801b1a1a75eeabb4, 0x00203fe10b30d482, 0x47f73f78825dd166, 0x4b64d4e38ad2c8a9,
	0x3a2cf1331e76e058, 0x6869e6f904507fc7, 0x10b2661490dcb712, 0x0850ceb646e80457,
	0x116685b8dd6667d4, 0xb19dac9c6d702644, 0xce21a59ccd38b7b8, 0x9b50d1643069ee49,
	0xe6f0402511e261af, 0x1543b39bc80eb4f5, 0x5e2fe04708719a1a, 0xf0e5a5ae6c06a5c1,
	0x15a23fa6b5f9737f, 0x44014ba60b9f2bc1, 0x3eb7e4748df4fc86, 0xa5d1e13344edadae,
	0xcc5be0e4cf381cd4, 0x9562e24a3023bb27, 0xe72ab27a446ec96a, 0x023bd6a3fb7dd5fb,
	0x9a5162d84dfdddf8, 0x775e3c60ea489e0d, 0xe02c258a7e57f68f, 0x44ac2a62213a8905,
	0x05b652cb6ea0e4a4, 0x7d08b8afc5f7dfd4, 0xc91a60fea48eb034, 0xa0f9e679e57b17fd,
	0x6fe3b02fd7f3d4f7, 0xdbe9fffe6e05759b, 0x0c651606e28004f4, 0x317b5f9799e13faf,
	0xd37d607d2c52a9e0, 0x187a7ca0adea160b, 0x493ad44e6442a3d2, 0x17a5d705e9522f44,
	0xe776651b26862854, 0x0343661b56cb852d, 0x7d4da58f2b954daa, 0x4f521aedbb4f87b8,
	0xcbcb8f3a26ed3202, 0x3b3d20fc0ba1c253, 0x61e507635abf8fa8, 0x4735048f739557c6,
	0x1a907c3d5e4fdc7c, 0x77c266a3ac72b7af, 0x2148f985769dd1ff, 0x3a89112077c76fe1,
	0x5f1544959f00d129, 0xe9a5a411564d6804, 0xe6a9c496a2043f7c, 0x07730c1edce71c37,
	0xb6943023558f52cf, 0xa2ddd9f5b8b0303e, 0xac44b20a35e4cc05, 0xd7b7a25db922832a,
	0x44abf3410c2b6f25, 0x40b6c7c07a1c2754, 0x7985d513b7b3687c, 0x59f84ff9565d6c5c,
	0xcb45f59b1dab4f3b, 0xbd7ec606f67107c5, 0xc240d372733c8d1f, 0x12decd52b0d799ba,
	0x34e03a0f1786f296, 0x1b0d85bb11e5fdaf, 0x5254a74f5295fa5d, 0x12cba46cfd2a10ef,
	0x38afdb9845e88e0d, 0x24b5fc140abff972, 0xab2e07ba3d2a0859, 0xb18f7465bef05972,
	0xc15e5f13f44f28c9, 0x5b0249dbb768787e, 0xc8a8d5dd03f11e21, 0xa8e5347fc8478895,
	0x3cd171029c0ba5d2, 0x54c838e5c5b92ca4, 0x3ff1825637c21f23, 0x6b1a941c0202e8bb,
	0x3b8051c33718db64, 0x8bf080e164e6bd17, 0x2101c2643bb75286, 0x8c0d4d700ce898dd,
	0x377c802a60da42b7, 0x4f55862507dcd852, 0x3a0ce28d5d20c0e7, 0x93e70bf4a13d93d5,
	0x0e37445fc2df2bdc, 0x4c904489b6687e6e, 0x4e4336c2b4d80a7d, 0xd619af0ca0fa6ab4,
	0x9c9b69559a3c9141, 0xe914735d646e6435, 0x30401eaac2d9ae3b, 0xfaead7e9c0a21102,
	0xb580a68e4cdbd5f7, 0x733e0edff9f00fd1, 0xb96403d2a9c6fe21, 0x8f0fcb4b7449190d,
	0x7c5130667c78409c, 0x55c4dd3c4d45314c, 0x31ec65786e3b78a0, 0x9894215b1eefd220,
	0xed1e2e1e93f3ec92, 0x3ca88182e909e144, 0x4f3951f8ab45a64d, 0x6c5e4436725e6ba1,
	0x4a101f18d27a5d2c, 0x1a3e6930b188b943, 0x08a19dc09d26a17d, 0x97608aa76c0b6acc,
	0xfd3513210e4e2dc6, 0x7e9f43071e7b9adf, 0xd1c3d3d4714b2119, 0xc4284aed9dc32160,
	0x1e381be0e17d4ee2, 0x47c87331f8d1c0d9, 0x18b34ca99273920c, 0x50900d0141ad89eb,
	0x8d4b5aaf8ebd5990, 0x79cdd6bc43e5f945, 0xc00d4771e0ec39d3, 0x209b52f26ba17e7b,
	0xdf240411c7fb90d5, 0xdd21fc3e3836a8bd, 0x03b760071b7f6d7f, 0x83d7d2763ca4e3fb,
	0x9d972bf43b1e8995, 0x2318c987d23a08a7, 0x57b4371053465ced, 0xe14af125bca918a5,
	0x2c23956ea47c134f, 0x0f746e9e6465346b, 0x432007ad43d04cf0, 0x5e488034dcb66a51,
	0xee4431d2e688212d, 0x35bb063065c88d07, 0xf5814995832850ae, 0x52f747a9f1dfbf32,
	0x4e4fe516cb8db3ec, 0xc6022a772503eea2, 0x54cac312eddb63e6, 0x2c567d03665d2622,
	0x7db50bdd6371d4c2, 0x3617d4cc34a79236, 0x38b4201ec93a947c, 0x2db805bbad8f2a02,
	0x3d42d1c34891f46d, 0x896e43212e93c5bd, 0x93a85b9379222641, 0xca820ca2b6a55b6e,
	0xad2054536982a909, 0x8ff7b6c28b0cc7cd, 0xc723d7b8942f756c, 0x8a797c1730c50834,
	0xc545c0f678eb5b39, 0xd3acddf0a3f2d7e8, 0x4932281764b7a45d, 0x4d98fa07cb65ab18,
	0x6a0e3d53e0940527, 0x2aa8d11b8d6b5c6c, 0x2e92a6e53c34e897, 0xfdeccfd866f89c31,
	0x67e1c2e95e6fc15f, 0x3d6c883448f235cb, 0x0707a689d112c85e, 0x4466343e32b233de,
}
