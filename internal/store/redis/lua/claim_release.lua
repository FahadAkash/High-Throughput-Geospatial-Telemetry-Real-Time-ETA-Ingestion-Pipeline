-- claim_release.lua
-- Atomic compare-and-delete: releases a driver claim only if the current holder matches the rideID.
--
-- KEYS[1] = claim:{driverID}
-- ARGV[1] = expected rideID

local claimKey = KEYS[1]
local expectedRideID = ARGV[1]

local currentHolder = redis.call("GET", claimKey)

if currentHolder == expectedRideID then
    return redis.call("DEL", claimKey)
else
    return 0
end

