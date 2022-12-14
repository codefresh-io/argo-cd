health_status = {}

if obj.status ~= nil then
  if obj.status.healthy ~= nil then
    if obj.status.healthy == true {
      health_status.status = "Healthy"
      health_status.message = "Application is healthy according to amiok.com condition check"
    else
      health_status.status = "Degraded"
      health_status.message = "Application is degraded according to amiok.com condition check"
    end
  end
end

return health_status
