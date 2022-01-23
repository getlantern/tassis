local deviceKey = KEYS[1]
local identityDevicesKey = KEYS[2]
local oneTimePreKeysKey = KEYS[3]

local deviceId = ARGV[1]

local remainingDevices = redis.call("scard", identityDevicesKey)
if remainingDevices == 1 then
	redis.call("del", identityDevicesKey)
else
	redis.call("srem", identityDevicesKey, deviceId)
end

redis.call("del", deviceKey, oneTimePreKeysKey)
return 0