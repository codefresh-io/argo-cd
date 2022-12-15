health_status = {}

if obj.status ~= nil then
  if obj.status.healthy ~= nil then
    if obj.status.healthy == "healthy" then
      health_status.status = "Healthy"
      health_status.message = "Application is healthy according to amiok.com condition check"
      return health_status
    elseif obj.status.healthy == "unhealthy" then
      health_status.status = "Degraded"
      health_status.message = "Application is degraded according to amiok.com condition check"
      return health_status
    else 
      health_status.status = "Unknown"
      health_status.message = "Application health is unknown according to amiok.com condition check"
      return health_status
    end
  end
end

health_status.status = "Progressing"
health_status.message = "waiting for amiok.com condition check on Application"
return health_status

