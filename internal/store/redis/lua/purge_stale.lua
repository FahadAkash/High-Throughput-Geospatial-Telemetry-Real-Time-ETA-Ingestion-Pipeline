-- purge_stale.lua
-- Removes all drivers from the provided cell ZSETs whose timestamp is <= maxStaleTs.
--
-- KEYS[1..N] = cell:{h3hex}
-- ARGV[1] = maxStaleTs (Unix ms)

local maxStaleTs = ARGV[1]
local totalRemoved = 0

for i, cellKey in ipairs(KEYS) do
    if cellKey and cellKey ~= "" then
        local removed = redis.call("ZREMRANGEBYSCORE", cellKey, "-inf", maxStaleTs)
        totalRemoved = totalRemoved + removed
    end
end

return totalRemoved

