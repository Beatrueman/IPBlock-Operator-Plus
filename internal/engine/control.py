import logging
from flask import Flask, jsonify, request, Response
import subprocess
import datetime
import requests
import shlex
import re

app = Flask(__name__)

# log
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s',  handlers=[logging.StreamHandler()])


TARGET_PORT = 8081

def get_hashlimit_name(ip: str) -> str:
    return f"limit_{ip.replace('/', '_')}"

def run_cmd(cmd):
    result = subprocess.run(cmd, capture_output=True, text=True, check=True)
    return result.stdout

# 检查iptables规则是否存在
def iptables_rule_exists(ip):
    try:
        rules = run_cmd(["iptables-save"])
        name = get_hashlimit_name(ip)
        pattern = re.compile(rf"--hashlimit-name {re.escape(name)}")
        return bool(pattern.search(rules))
    except Exception as e:
        logging.error(f"Error checking iptables rules: {e}")
        return False

def add_limit_rule(ip):
    hashlimit_name = f"{ip.replace('/', '_')}"
    # IP 每分钟最多发起 10 个新连接（可突发20次），允许通过
    limit_rule = [
        "iptables", "-A", "INPUT", "-s", ip, "-p", "tcp",
        "--dport", str(TARGET_PORT),
        "-m", "state", "--state", "NEW",
        "-m", "hashlimit", "--hashlimit", "10/min",
        "--hashlimit-name", f"limit_{hashlimit_name}",
        "--hashlimit-burst", "20",
        "--hashlimit-mode", "srcip",
        "-j", "ACCEPT"
    ]

    # 超过限制的 IP，直接 DROP
    drop_rule = [
        "iptables", "-A", "INPUT", "-s", ip, "-p", "tcp",
        "--dport", str(TARGET_PORT), "-j", "DROP"
    ]

    subprocess.check_call(limit_rule)
    subprocess.check_call(drop_rule)

def remove_limit_rule(ip):
    try:
        hashlimit_name = get_hashlimit_name(ip)
        rules = run_cmd(["iptables-save"])
        lines = rules.strip().splitlines()

        hashlimit_pattern = re.compile(rf'--hashlimit-name {re.escape(hashlimit_name)}')
        drop_pattern = re.compile(rf'-A INPUT -s {re.escape(ip)} .* -j DROP')

        match_to_delete = []
        for line in lines:
            if hashlimit_pattern.search(line) or drop_pattern.search(line):
                logging.info(f"Matched for deletion: {line}")
                match_to_delete.append(line)

        for rule in match_to_delete:
            cmd = ["iptables"] + shlex.split(rule.replace("-A", "-D", 1))
            logging.info(f"Deleting rule: {' '.join(cmd)}")
            subprocess.run(cmd, check=True)

        # 验证是否删除成功
        updated_rules = run_cmd(["iptables-save"])
        if hashlimit_name in updated_rules:
            logging.warning(f"Rule for {ip} still exists after attempted deletion.")

    except Exception as e:
        logging.error(f"Failed to remove rules for {ip}: {e}")



@app.route('/execute', methods=['GET'])
def execute_command():
    # 获取当前时间的小时数，格式化为日志时间格式，例如："25/Aug/2024:15"
    current_time = datetime.datetime.now().strftime("%d/%b/%Y:%H")

    # 构建命令
    command = f"cat /var/log/mirror/access.log | grep -a '{current_time}' | awk '{{print $1}}' | sort | uniq -c | sort -rn | head -10"

    try:
        # 执行命令
        result = subprocess.run(command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)

        if result.stderr:
            logging.error(f"Command error: {result.stderr.decode('utf-8')}")

        # 如果命令执行失败，返回错误信息
        if result.returncode != 0:
            logging.error(f"Command failed with return code: {result.returncode}")
            return jsonify({'error': result.stderr.decode('utf-8')}), 500

        # 解析命令输出
        results = []
        lines = result.stdout.decode('utf-8').split('\n')
        logging.info(f"Raw output from the command: {result.stdout.decode('utf-8')}")

        for line in lines:
            if line.strip():  # 过滤掉空行
                try:
                    count, ip = line.split(maxsplit=1)
                    results.append({'ip': ip, 'count': int(count)})
                except ValueError as e:
                    logging.error(f"Error parsing line: '{line}', Error: {e}")

        logging.info(f"Processed results: {results}")
        return jsonify(results)

    except Exception as e:
        logging.error(f"An error occurred while executing the command: {str(e)}")
        return jsonify({'error': str(e)}), 500

# 对 IP 进行封禁
@app.route('/ban', methods=['POST'])
def ban_ips():
    # 从请求的JSON主体中获取IP列表
    ip_data_list = request.json
    if not ip_data_list:
        logging.warning("Missing IP data in request body!")
        return jsonify({"error": "Missing IP data in request body"}), 400

    results = []
    for ip_data in ip_data_list:
        ip = ip_data.get("ip")
        count = ip_data.get("count", 0)
        if count >= 250:
            try:
                # 对每个IP执行curl命令
                curl_command = f"curl -s '198.18.114.2:8080/update?cidr={ip}&ban_type=1&ban_time=36000'"
                curl_output = subprocess.check_output(curl_command, shell=True, text=True)
                # 检查curl命令的输出
                if f"Successfully added {ip} to banned list" in curl_output or f'{ip} have been updated' in curl_output:
                    logging.info(f"Succeed to ban {ip}")
                    results.append({"ip": ip, "count": count, "status": "success"})
                else:
                    # 添加日志
                    logging.error(f"Failed to ban {ip}: cur_output: {curl_output}")
                    results.append({"ip": ip, "count": count, "status": "failed"})
            except subprocess.CalledProcessError as e:
                logging.error(f"Failed to ban {ip}: {e}")
                results.append({"ip": ip, "count": count, "status": "failed", "error": str(e)})
        else:
            logging.info(f"Skipped IP: {ip}, count: {count}")
            results.append({"ip": ip, "count": count, "status": "skipped"})

    return jsonify(results)

# 对 IP 进行限流
@app.route('/limit', methods=['GET'])
def limit_ip():
    ip = request.args.get("ip")
    if not ip:
        logging.warning("Missing 'ip' query parameter!")
        return jsonify({"error": "Missing 'ip' query parameter"}), 400

    try:
        if iptables_rule_exists(ip):
            logging.info(f"Limit rule for {ip} already exists, skipping insertion")
            return jsonify({"ip": ip, "status": "already_limited"})
        else:
            add_limit_rule(ip)
            logging.info(f"Added limit rule for {ip}")
            return jsonify({"ip": ip, "status": "limited"})
    except subprocess.CalledProcessError as e:
        logging.error(f"Failed to limit {ip}: {e}")
        return jsonify({"ip": ip, "status": "failed", "error": str(e)})

# 对 IP 进行解限流
@app.route('/unlimit', methods=['GET'])
def unlimit_ip():
    ip = request.args.get("ip")
    if not ip:
        logging.warning("Missing 'ip' query parameter!")
        return jsonify({"error": "Missing 'ip' query parameter"}), 400

    try:
        remove_limit_rule(ip)
        logging.info(f"Removed limit rule for {ip}")
        return jsonify({"ip": ip, "status": "unlimited"})
    except Exception as e:
        logging.error(f"Failed to remove limit for {ip}: {e}")
        return jsonify({"ip": ip, "status": "failed", "error": str(e)})

# 查看当前所有的限流规则IP列表
@app.route('/limits', methods=['GET'])
def list_limited_ips():
    try:
        rules = run_cmd(["iptables-save"])
        pattern = re.compile(r"--hashlimit-name (limit_[\w\._]+)")
        ips = set()

        for match in pattern.finditer(rules):
            name = match.group(1).replace("limit_", "")
            ip = name.replace("_", "/") if "/" not in name and "_" in name else name
            ips.add(ip)

        return jsonify({"limited_ips": list(ips)})
    except Exception as e:
        logging.error(f"Failed to list limits: {e}")
        return jsonify({"error": str(e)}), 500


# 代理转发
TARGET_HOST = "http://198.18.114.2:8080"

@app.route('/update')
def update_proxy():
    try:
        cidr = request.args.get("cidr")
        ban_type = request.args.get("ban_type")
        ban_time = request.args.get("ban_time")

        remote_url = f"{TARGET_HOST}/update?cidr={cidr}&ban_type={ban_type}&ban_time={ban_time}"
        response = requests.get(remote_url, timeout=3)

        return (response.text, response.status_code)

    except requests.exceptions.RequestException as e:
        app.logger.error(f"Failed to proxy update request: {e}")
        return jsonify({"error": "Failed to forward request", "detail": str(e)}), 502

@app.route('/remove')
def proxy_remove():
    try:
        cidr = request.args.get("cidr")

        remote_url = f"{TARGET_HOST}/remove?cidr={cidr}"
        resp = requests.get(remote_url, timeout=3)
        return Response(resp.content, status=resp.status_code, content_type=resp.headers.get("Content-Type"))
    except requests.exceptions.RequestException as e:
        app.logger.error(f"Failed to proxy remove request: {e}")
        return jsonify({"error": "Failed to forward request", "detail": str(e)}), 502

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=9521)
