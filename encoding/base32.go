package encoding

import "encoding/base32"

var HumanFriendlyBase32Encoding = base32.NewEncoding("ybndrfg8ejkmcpqxot1uw2sza345h769").WithPadding(base32.NoPadding)
