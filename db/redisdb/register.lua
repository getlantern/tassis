local deviceKey = KEYS[1]
local identityDevicesKey = KEYS[2]
local oneTimePreKeysKey = KEYS[3]

local newSignedPreKey = ARGV[1]
local deviceId = ARGV[2]

local oldSignedPreKey = redis.call("hget", deviceKey, "signedPreKey")

redis.call("sadd", identityDevicesKey, deviceId)
if newSignedPreKey ~= oldSignedPreKey then
	redis.call("hset", deviceKey, "signedPreKey", newSignedPreKey)
	redis.call("del", oneTimePreKeysKey)
	return 0
end

return 1