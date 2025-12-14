# README

# IPBlock-Operator-Plus

## 项目简介

**IPBlock-Operator-Plus** 是一个基于 Kubernetes 的 Operator，构建了一个插件化、模块化的恶意 IP 封禁管理平台。该项目通过自定义资源（IPBlock CR）实现对恶意 IP 的声明式管理，支持手动和自动限流与封禁功能。其架构高度可扩展，便于集成多种触发器（Trigger）和通知机制（Notify），从而实现智能化的安全防护与运维管理。

## 项目架构

**架构图**

![IPBlock-Operator-Plus架构图](https://gitee.com/beatrueman/images/raw/master/20250702205541724.png)

IPBlock-Operator-Plus 项目由以下五个核心模块组成：

- **Controller（控制器）**

​	负责核心业务逻辑的调度和协调，监听 Kubernetes 中 IPBlock 自定义资源的变化，维护和管理其生命周期。它是整个项目的核心部分，协调各个模块协同工作。

- **Engine（封禁后端）**

  负责封禁命令的实际执行。目前支持接入多种封禁机制（如XDP，iptables等）。通过插件化设计，便于快速扩展和替换不同的封禁技术方案。
- **Notify（通知机制）**

​	负责将封禁、解封等事件以多种方式通知给运维人员或其他系统。现支持飞书通知渠道，开发人员可通过自定义插件（实现接口）灵活添加更多通知方式。

- **Trigger（触发器）**

  事件触发中心，负责监听外部告警系统或业务事件（如 Grafana Alert等），并根据 Alert 策略触发相关封禁操作，自动创建 IPBlock CR，且支持自动解封。模块设计也方便开发人员接入多种告警来源。
- **Policy（封禁策略）**

  定义 IP 封禁的判定规则和执行策略。现支持白名单机制，保障了封禁行为的精准与合理。

## 项目部署

### 环境要求

- go version v1.24.0+
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.
- Python 3
- Make

项目支持 **Helm** 和 **Make**两种部署方式

### 封禁后端部署

封禁后端需要在监测目标机器上手动执行`python3 ../../ipblock-operator/internal/engine/control.py`

最好将其制作成Service，保证后台持久运行。

这里提供`get_ip.service`文件供参考。

```shell
[Unit] 
Description=Get IP Service 
After=network.target 
[Service] 
User=root 
WorkingDirectory=/root/yiiong/get_ip  # control.py所在目录
Environment="PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
ExecStart=/bin/bash -c 'source /root/yiiong/get_ip/venv/bin/activate && exec python3 control.py' # 运行Python程序，注意文件路径
Restart=always 
[Install] 
WantedBy=multi-user.target
```

### Helm

项目的`helm/`下存放了IPBlock-Operator-Plus的helm chart，在安装前，请先填写`values.yaml`

```yaml
# values.yaml
image:
  repo: beatrueman/ipblock-operator
  tag: "8.1"
  pullPolicy: IfNotPresent

config:
  gatewayHost: ""                                    # 封禁后端 URL
  engine: ""                                         # 可选: xdp, iptables
  whiteList: |										                   # IP 白名单，支持在 ConfigMap中动态更新
    1.2.3.4
  notifyType: ""                                     # 可选: lark
  notifyWebhookURL: ""                               # larkRobot Webhook
  notifyTemplate:                                    # larkRobot发送的card消息模板
    ban: "/templates/lark/ban.json"
    resolve: "templates/lark/resolve.json"
    common: "/templates/lark/common.json"
  triggers:                                          # 触发器，目前仅支持 Grafana
    - name: grafana
      addr: ":8090"
      path: "/trigger/grafana"
```

填写完成后，安装helm chart即可

注意需要将IPblock-Operator部署在`default`命名空间下。

```
helm install ipblock-operator .
```

### Make

#### 在集群上部署

##### 将 CRD 安装到集群中

```shell
make install
```

##### 将 Manager 部署到集群中

填写`../../IPBlock-Operator-Plus/config/default`下的`configmap.yaml`

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ipblock-operator-config
data:
  gatewayHost: ""                                             # 封禁后端 URL
  engine: ""                                                  # 可选: xdp, iptables
  trigger: |                                                  # 触发器，目前仅支持 Grafana
    - name: grafana
      addr: ":8090"
      path: "/trigger/grafana"
  whitelist: |                                                # IP 白名单，支持在 ConfigMap中动态更新
    1.2.3.4
  notifyType: ""                                              # 可选: lark
  notifyWebhookURL: ""                                        # larkRobot Webhook
  notifyTemplate_ban: "/templates/lark/ban.json"              # larkRobot发送的card消息模板，请勿更改路径
  notifyTemplate_resolve: "/templates/lark/resolve.json"
  notifyTemplate_common: "/templates/lark/common.json"
```

> **注意：** 如果您遇到 RBAC 错误，您可能需要授予自己 cluster-admin 权限或以 admin 身份登录。

```shell
make deploy
```

##### **创建样例**

```shell
kubectl apply -k config/samples/
```

#### 卸载

##### 从集群中删除实例（CRs）

```shell
kubectl delete ipblock --all
```

##### 从集群中删除API（CRDs）

```shell
make uninstall
```

##### 从集群中删除控制器

```shell
make undeploy
```

## 配置项说明

### ConfigMap

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ipblock-operator-config
data:
  gatewayHost: ""                                             # 封禁后端 URL
  engine: ""                                                  # 可选: xdp, iptables
  trigger: |                                                  # 触发器，目前仅支持 Grafana
    - name: grafana
      addr: ":8090"
      path: "/trigger/grafana"
  whitelist: |                                                # IP 白名单，支持在 ConfigMap中动态更新
    1.2.3.4
  notifyType: ""                                              # 可选: lark
  notifyWebhookURL: ""                                        # larkRobot Webhook
  notifyTemplate_ban: "/templates/lark/ban.json"              # larkRobot发送的card消息模板，请勿更改路径
  notifyTemplate_resolve: "/templates/lark/resolve.json"
  notifyTemplate_common: "/templates/lark/common.json"
```

### values.yaml

```yaml
# values.yaml
image:
  repo: beatrueman/ipblock-operator
  tag: "8.1"
  pullPolicy: Always

config:
  gatewayHost: ""                                    # 封禁后端 URL
  engine: ""                                         # 可选: xdp, iptables
  whiteList: |										                   # IP 白名单，支持在 ConfigMap中动态更新
    1.2.3.4
  notifyType: ""                                     # 可选: lark
  notifyWebhookURL: ""                               # larkRobot Webhook
  notifyTemplate:                                    # larkRobot发送的card消息模板
    ban: "/templates/lark/ban.json"
    resolve: "templates/lark/resolve.json"
    common: "/templates/lark/common.json"
  ServiceType: NodePort
  triggers:                                          # 触发器，目前仅支持 Grafana
    - name: grafana
      addr: ":8090"
      path: "/trigger/grafana"
```

### Trigger配置

#### Grafana

##### 字段介绍

|字段|说明|必需|
| :---| :---------------------| :---|
|name|触发器名称，当前支持 `grafana`|是|
|addr|监听地址和端口，例如 `":8090"`|是|
|path|Webhook请求路径，例如 `/trigger/grafana`|是|

##### Grafana Alert配置

**配置联络点**

将配置好的URL填入联络点的URL中。

举例：`http://<your-ip>:<NodePort>/trigger/grafana`​

![image-20250703145604574](https://gitee.com/beatrueman/images/raw/master/20250703145604718.png)

**配置警报规则**

自定义警报规则，并选择联络点为刚才配置好的联络点。

本例为1min内若访问次数超过80，则触发警报。

![image-20250703145806206](https://gitee.com/beatrueman/images/raw/master/20250703145846163.png)

封禁时长可通过标签设置

```
duration: 10[s/m/h]
```

![image](https://gitee.com/beatrueman/images/raw/master/20251214235842092.png)

### Notigy配置

目前仅支持飞书Lark，后续将添加更多，如邮件、钉钉、企业微信等。

#### Lark

添加自定义机器人，并将获取到的Webhook地址填入配置的`notifyWebhookURL`即可。

![image-20250703150204187](https://gitee.com/beatrueman/images/raw/master/20250703150204303.png)

如果Trigger选择Grafana，Lark机器人通知内容会发送description的内容。

![image](https://gitee.com/beatrueman/images/raw/master/20251214235846993.png)

![image](https://gitee.com/beatrueman/images/raw/master/20251214235850775.png)

## 使用示例

### 创建一个 IPBlock 资源

```yaml
apiVersion: ops.yiiong.top/v1
kind: IPBlock
metadata:
  name: test-ipblock
spec:
  ip: "1.2.3.4"                       # 支持单IP / CIDR形式
  reason: "模拟异常请求"               # 封禁原因
  source: "manual"                    # 封禁源
  by: "admin"                         # 封禁者
  duration: "10m"                     # 封禁时长，当字段为空时，永久封禁  
```

CR创建成功后，如接入飞书，会向用户发起通知。

![image-20250703150503557](https://gitee.com/beatrueman/images/raw/master/20250703150503733.png)

![image](https://gitee.com/beatrueman/images/raw/master/20251214235855089.png)

### 对IP解封

```yaml
apiVersion: ops.yiiong.top/v1
kind: IPBlock
metadata:
  name: test-ipblock
spec:
  ip: "1.2.3.4"                      
  reason: "模拟异常请求"                
  source: "manual"                    
  by: "admin"                         
  unblock: true                      # 解封字段
```

成功后，飞书会通知用户

![image-20250703150639796](https://gitee.com/beatrueman/images/raw/master/20250703150639879.png)

### 手动强制封禁

```yaml
apiVersion: ops.yiiong.top/v1
kind: IPBlock
metadata:
  name: test-ipblock
spec:
  ip: "1.2.3.4"                      
  reason: "模拟异常请求"                
  source: "manual"                    
  by: "admin"                         
  trigger: true                      # 重复封禁
```

### 白名单跳过

当在配置文件中指定了`WhiteList`（支持单IP / CIDR），CR会检测封禁IP是否在白名单中，如在则跳过。
