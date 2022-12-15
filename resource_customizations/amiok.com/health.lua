health_status = {}

if obj.status ~= nil then
  if obj.status.healthy ~= nil then
    if obj.status.healthy == true then
      health_status.status = "Healthy"
      health_status.message = "Application " .. obj.metadata.name " is healthy according to amiok.com condition check"
    else
      health_status.status = "Degraded"
      health_status.message = "Application " .. obj.metadata.name " is degraded according to amiok.com condition check"
    end
  end
end

health_check.status = "Progressing"
health_check.message = "waiting for amiok.com condition check on Application " .. obj.metadata.name
return health_status
