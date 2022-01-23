local deviceKey = KEYS[1]
local oneTimePreKeysKey = KEYS[2]

local signedPreKey = redis.call("hget", deviceKey, "signedPreKey")
local oneTimePreKey = redis.call("lpop", oneTimePreKeysKey)

return {signedPreKey, oneTimePreKey}