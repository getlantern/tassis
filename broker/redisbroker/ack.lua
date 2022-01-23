local offsetKey = KEYS[1]
local newOffset = ARGV[1]

local oldOffset = redis.call("get", offsetKey)
if oldOffset then
	local newOffsetParts = {}
	for str in string.gmatch(newOffset, "[^-]+") do
		table.insert(newOffsetParts, str)
	end
	local oldOffsetParts = {}
	for str in string.gmatch(oldOffset, "[^-]+") do
		table.insert(oldOffsetParts, str)
	end
	if newOffsetParts[1] > oldOffsetParts[1] or (newOffsetParts[1] == oldOffsetParts[1] and newOffsetParts[2] > oldOffsetParts[2]) then
		redis.call("set", offsetKey, newOffset)
		return 1
	end
else
	redis.call("set", offsetKey, newOffset)
	return 1
end

return 0