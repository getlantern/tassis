local identityToNumberKey = KEYS[1]
local numberToIdentityKey = KEYS[2]
local numberToShortNumberKey = KEYS[3]
local shortNumberToNumberKey = KEYS[4]
local lastRegistrationTimeKey = KEYS[5]
local registrationTokensKey = KEYS[6]

local identityKey = ARGV[1]
local newNumber = ARGV[2]
local newShortNumber = ARGV[3]
local requiredTokens = tonumber(ARGV[4])

local number = redis.call("hget", identityToNumberKey, identityKey)
if number and number ~= nil and number ~= '' then
	-- already registered
	local shortNumber = redis.call("hget", numberToShortNumberKey, number)
	return {number, shortNumber}
end

local existingIdentityKey = redis.call("hget", numberToIdentityKey, newNumber)
if existingIdentityKey then
	-- number already belongs to a different identity key
	return -1
end

-- rate limiting
if requiredTokens > 0 then
	local now = ARGV[5]
	local lastRegistrationTime = redis.call("getset", lastRegistrationTimeKey, now)
	if lastRegistrationTime then
		local delta = now - lastRegistrationTime - requiredTokens
		local availableTokens = redis.call("incrby", registrationTokensKey, delta)
		if availableTokens < 0 then
			-- This client needs to be rate limited, return the number of tokens we're short.
			-- We do this prior to completing the registration. Once the client has slept the
			-- necessary amount of time, it will call us again with rate limiting disabled in
			-- order to complete the registration.
			return -1 * availableTokens
		end
	end
end

-- new registration
redis.call("hset", identityToNumberKey, identityKey, newNumber)
redis.call("hset", numberToIdentityKey, newNumber, identityKey)
redis.call("hset", numberToShortNumberKey, newNumber, newShortNumber)
redis.call("hset", shortNumberToNumberKey, newShortNumber, newNumber)

return {newNumber, newShortNumber}