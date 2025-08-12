# Symphony 端到端（E2E）测试计划

## 1. 测试目标

本测试计划旨在验证Symphony系统在真实模拟环境中的端到端功能、可靠性、安全性和性能。测试的重点是从用户实际应用场景出发，确保系统各组件（Symphony Server, Remote Agent, Redis, MQTT Broker）能够协同工作，完成复杂的部署和管理任务。

本文档将取代旧的测试计划，并更清晰地划分**单元/集成测试**与**端到端（E2E）测试**。

## 2. 测试范围

### 2.1. 单元与集成测试 (Unit &amp; Integration Tests)

这部分测试关注单个组件内部逻辑的正确性以及组件间的直接交互。它们是E2E测试的基础，应在开发过程中持续运行。

*   **Symphony主服务**:
    *   API端点功能测试（如 `/health`, `/targets`, `/solutions`）。
    *   K8S资源（Target, Solution, Instance）的控制器逻辑。
    *   Redis队列的入队/出队操作。
*   **Remote Agent**:
    *   HTTP和MQTT通信Binding的连接和重试逻辑。
    *   Provider（如script, helm）的执行逻辑。
*   **协议层**:
    *   HTTP/MQTT消息格式和序列化。
    *   MQTT Topic订阅和发布逻辑。

> **说明**: 原有的 `symphony-backend-test-plan.md` 中的大部分测试用例属于此类别，可作为单元/集成测试实现的参考。

### 2.2. 端到端测试 (End-to-End Tests)

这部分测试是本计划的核心。它将整个Symphony系统视为一个黑盒，从用户角度验证完整的业务流程。所有E2E测试都应在自动化的、隔离的Kubernetes环境（如Minikube）中执行。

## 3. 端到端（E2E）测试场景

以下是核心的E2E测试场景，旨在覆盖Symphony的关键用户故事。

---

### **场景1：完整应用部署生命周期**

*   **用户故事**: 作为一名DevOps工程师，我希望能够使用Symphony将一个包含Web前端、后端API和数据库的三层应用部署到目标环境中，并对其进行全生命周期管理。
*   **测试流程**:
    1.  **环境准备**: 启动一个全新的Minikube集群，并部署Symphony服务。
    2.  **资源创建**:
        *   创建一个`Target`资源，指向Minikube集群。
        *   创建一个包含三个组件（nginx, a-nodejs-app, redis）的`Solution`资源，并定义组件间的依赖关系。
        *   创建一个`Instance`资源，将该`Solution`部署到`Target`。
    3.  **部署验证**:
        *   **验证点**: 确认Instance状态最终变为`Succeeded`。
        *   **验证点**: 确认nginx, nodejs, redis的Pod在Minikube中正常运行。
        *   **验证点**: 通过端口转发或Ingress访问nginx，确认Web应用可以正常访问并能连接到后端API和Redis。
    4.  **滚动更新**:
        *   更新`Solution`中的nodejs应用镜像版本。
        *   **验证点**: 确认Symphony触发了滚动更新，旧的Pod被替换，新的Pod启动。
        *   **验证点**: 在更新过程中，应用访问保持可用（允许短暂中断）。
    5.  **资源清理**:
        *   删除`Instance`资源。
        *   **验证点**: 确认与该Instance相关的所有Pod、Service等K8s资源被完全清理。
        *   删除`Target`和`Solution`。

---

### **场景2：多集群/多目标管理**

*   **用户故事**: 作为一名平台管理员，我需要使用单一Symphony实例来管理分布在不同位置的多个Kubernetes集群（开发、测试、生产）。
*   **测试流程**:
    1.  **环境准备**: 启动一个Symphony主集群，并额外创建两个独立的Minikube集群作为`dev-cluster`和`staging-cluster`。
    2.  **目标注册**:
        *   在Symphony主集群中创建两个`Target`资源，分别指向`dev-cluster`和`staging-cluster`。
        *   在`dev-cluster`和`staging-cluster`上分别部署Remote Agent。
    3.  **并行部署**:
        *   创建一个`Solution`（例如，一个简单的Web应用）。
        *   创建两个`Instance`，将同一个`Solution`分别部署到`dev-target`和`staging-target`。
    4.  **隔离性验证**:
        *   **验证点**: 确认两个环境中都成功部署了应用。
        *   **验证点**: 在`dev-cluster`中删除Pod，确认`staging-cluster`中的应用不受任何影响。
        *   **验证点**: 确认每个Target的状态和拓扑信息被独立、准确地收集和展示。
    5.  **统一管理**:
        *   更新`Solution`。
        *   **验证点**: 确认两个`Instance`都会被更新，证明可以通过中心化的Symphony管理多个目标。

---

### **场景3：故障恢复与可靠性**

*   **用户故事**: 作为一名SRE，我需要确保Symphony系统在遇到网络抖动或组件故障时具有韧性，能够自动恢复，且任务不丢失。
*   **测试流程**:
    1.  **环境准备**: 部署一个完整的Symphony环境（Symphony Server, Remote Agent, Redis）。
    2.  **网络中断测试 (HTTP/MQTT)**:
        *   部署一个`Instance`并使其处于稳定运行状态。
        *   使用网络工具（如`iptables`或`tc`）断开Remote Agent与Symphony Server之间的网络连接。
        *   **验证点**: 确认Agent日志中出现连接错误和重试逻辑。
        *   恢复网络连接。
        *   **验证点**: 确认Agent能自动重连，并恢复正常心跳和任务轮询。
    3.  **Agent宕机恢复**:
        *   在Symphony中创建一个新的`Instance`，使其任务进入Redis队列，但尚未被Agent获取。
        *   停止Remote Agent进程。
        *   **验证点**: 确认任务保留在Redis队列中。
        *   重启Remote Agent。
        *   **验证点**: 确认Agent启动后能成功获取并执行之前队列中的任务。
    4.  **Redis服务故障**:
        *   停止Redis服务。
        *   尝试创建一个新的`Instance`。
        *   **验证点**: 确认Symphony API返回错误，或Instance处于`Pending`状态，系统优雅地处理了后端依赖故障。
        *   恢复Redis服务。
        *   **验证点**: 确认系统恢复正常，之前失败的`Instance`可以被成功处理。

---

### **场景4：证书自动轮换（Cert Rotation）**

*   **用户故事**: 作为一名安全管理员，我需要确保Remote Agent的客户端证书能够自动轮换续期，避免因证书过期导致服务中断。
*   **测试流程**:
    1.  **环境准备**:
        *   部署Symphony，并配置一个**生命周期极短**的CA和客户端证书（例如，有效期5分钟）。
    2.  **初始部署**:
        *   使用该短生命周期证书启动Remote Agent，并成功部署一个`Instance`。
        *   **验证点**: 确认应用正常运行，Agent与Server通信正常。
    3.  **触发与验证轮换**:
        *   等待，直到证书接近过期（例如，剩余有效期小于60%）。
        *   **监控点**: 观察Agent日志，确认其发起了新的证书请求。
        *   **监控点**: 观察Symphony日志，确认其签发了新证书。
        *   **验证点**: 在轮换过程中，持续请求应用，确认服务不中断。
    4.  **使用新证书验证**:
        *   在旧证书过期后，尝试更新`Instance`。
        *   **验证点**: 确认更新操作成功，证明Agent已无缝切换到新证书。

---

### **场景5：复杂工作流与依赖编排**

*   **用户故事**: 作为一名应用开发者，我需要部署一个包含多个微服务的复杂应用，并确保它们按正确的依赖顺序启动。
*   **测试流程**:
    1.  **环境准备**: 部署Symphony。
    2.  **定义复杂Solution**:
        *   创建一个`Solution`，包含三个组件：`config-db` (一个初始化数据库的脚本), `api-service` (依赖`config-db`), `webapp` (依赖`api-service`)。
        *   在`Solution`中明确定义`dependsOn`关系。
    3.  **部署与验证**:
        *   创建`Instance`部署该`Solution`。
        *   **验证点**: 监控部署过程，确认组件严格按照`config-db` -> `api-service` -> `webapp`的顺序执行。
        *   **验证点**: 检查`api-service`的日志，确认它在启动时数据库已准备就绪。
        *   **验证点**: 最终所有组件都成功部署，整个应用可正常访问。

## 4. 测试执行与自动化

*   **自动化框架**: 所有E2E测试都应基于`test/e2e/remote-agent-integration`中的现有框架进行扩展。
*   **测试脚本**: 使用Go测试框架编写测试用例，并利用`run_tests.sh`脚本进行驱动。
*   **CI/CD集成**: 将E2E测试套件集成到CI/CD流水线中，在每次代码合并前或每日构建时自动运行。
*   **测试报告**: 每次执行后生成详细的测试报告，包括成功/失败的用例、执行日志和性能指标。
