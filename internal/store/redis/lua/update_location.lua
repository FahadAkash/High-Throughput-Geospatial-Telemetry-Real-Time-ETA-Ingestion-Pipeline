-- update_location.lua
-- Atomically transitions a driver from an old H3 cell to a new H3 cell
-- and updates the driver's current metadata hash with an expiration TTL.
--
-- KEYS[1] = cell:{newCell}
-- KEYS[2] = cell:{oldCell} (can be identical to KEYS[1] or empty string if unchanged / first ping)
-- KEYS[3] = drv:{driverID}
--
-- ARGV[1] = driverID
-- ARGV[2] = timestamp (Unix ms)
-- ARGV[3] = lat (string float)
-- ARGV[4] = lng (string float)
-- ARGV[5] = newCell (hex string)
-- ARGV[6] = status (string, e.g. "available")
-- ARGV[7] = ttl_ms (hash PX expiry in milliseconds)

local newCellKey = KEYS[1]
local oldCellKey = KEYS[2]
local drvKey = KEYS[3]

local driverID = ARGV[1]
local ts = ARGV[2]
local lat = ARGV[3]
local lng = ARGV[4]
local newCell = ARGV[5]
local status = ARGV[6]
local ttlMs = tonumber(ARGV[7])

-- If the driver moved to a different cell, remove them from the old cell ZSET
if oldCellKey and oldCellKey ~= "" and oldCellKey ~= newCellKey then
    redis.call("ZREM", oldCellKey, driverID)
end

-- Add / update the driver in the new cell ZSET with score = timestamp
redis.call("ZADD", newCellKey, ts, driverID)

-- Update driver metadata hash
redis.call("HSET", drvKey,
    "lat", lat,
    "lng", lng,
    "cell", newCell,
    "ts", ts,
    "status", status
)

-- Set TTL on driver state hash
if ttlMs and ttlMs > 0 then
    redis.call("PEXPIRE", drvKey, ttlMs)
end

return 1

