# 更新日志

## 2025/12/16

镜像更新至`beatrueman/ipblock-operator:8.2`

- fix: 修正Grafana CR patch逻辑，保证active、skipped、failed阶段的CR不会被再次patch，避免短时重复封禁


## 2025/12/15

镜像更新至`beatrueman/ipblock-operator:8.1`

- 支持通过Grafana Alert设置`duration: x[s/m/h]`标签控制封禁时长
- 修改Lark Bot通知内容为Grafana Alert的description
- IPBlock CR的status增加BanCount，记录封禁次数
- 增加一个K8s Cronjob，用来清理超过10d的IPBlock CR资源
- bugfix：当Grafana触发时，补充expired状态流转，保证当一个IP被解封后，后期可再次封禁