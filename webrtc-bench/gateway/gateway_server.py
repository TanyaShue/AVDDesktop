# Copyright (C) 2026 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
import argparse
import asyncio
import sys
import os
from pathlib import Path

# Add the proto/ directory to sys.path to allow absolute imports between sibling proto stubs
# (e.g., rtc_service_v2_pb2 importing ice_config_pb2).
proto_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "proto")
if proto_dir not in sys.path:
    sys.path.insert(0, proto_dir)

import aiohttp
from aiohttp import web
import grpc
import json
import logging

# Add local path to package import context to resolve generated proto modules
proto_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "proto")
sys.path.insert(0, proto_dir)
from proto import emulator_controller_pb2 as ec
from proto import emulator_controller_pb2_grpc as ec_grpc
from proto import rtc_service_v2_pb2 as rtc
from proto import rtc_service_v2_pb2_grpc as rtc_grpc
from proto import ice_config_pb2 as ice
from proto import rtc_service_v2_new_pb2 as rtc_new
from proto import rtc_service_v2_new_pb2_grpc as rtc_new_grpc

# Configure Logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(filename)s:%(lineno)d (%(funcName)s): %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)

# Properties dictionary populated by parsing the discovery file
DISCOVERY_PROPS = {}
EMULATOR_CHANNEL = None
VIDEOBRIDGE_CHANNEL = None


def parse_discovery_file(path):
    props = {}
    if not path or not os.path.exists(path):
        logging.warning(f"Discovery file not found or not specified at: {path}")
        return props
    try:
        with open(path, "r") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or line.startswith(";"):
                    continue
                if "=" in line:
                    k, v = line.split("=", 1)
                    props[k.strip()] = v.strip()
        logging.info(f"Loaded discovery properties from {path}")
    except Exception as e:
        logging.error(f"Failed to read discovery file: {e}")
    return props


def get_emulator_metadata():
    token = DISCOVERY_PROPS.get("grpc.token", "")
    if token:
        return [("authorization", f"Bearer {token}")]
    return []


def get_videobridge_metadata(token_arg):
    if token_arg:
        return [("authorization", f"Bearer {token_arg}")]
    return []


async def handle_status(request):
    """
    GET /api/v1/emulator/status
    Returns the hardware configurations and status of the emulator.
    """
    if not EMULATOR_CHANNEL:
        return web.json_response({"error": "No channel to emulator"}, status=503)

    try:
        stub = ec_grpc.EmulatorControllerStub(EMULATOR_CHANNEL)
        metadata = get_emulator_metadata()
        # Call getStatus
        status_res = await stub.getStatus(
            ec.google_dot_protobuf_dot_empty__pb2.Empty(), metadata=metadata
        )

        # Convert platformConfig map to a regular python dictionary
        platform_config = dict(status_res.platformConfig)

        # Extract vmConfig details
        vm_config = {
            "hypervisorType": status_res.vmConfig.hypervisorType if status_res.vmConfig else "unknown",
            "numberOfCpuCores": status_res.vmConfig.numberOfCpuCores if status_res.vmConfig else 0,
            "ramSizeBytes": status_res.vmConfig.ramSizeBytes if status_res.vmConfig else 0,
        }

        response_data = {
            "version": status_res.version,
            "uptime": status_res.uptime,
            "booted": status_res.booted,
            "hardwareConfig": platform_config,  # Deprecated, kept for backwards compatibility
            "platformConfig": platform_config,  # New preferred field
            "vmConfig": vm_config,
        }
        return web.json_response(response_data)
    except Exception as e:
        logging.error(f"Error fetching status from emulator: {e}")
        return web.json_response({"error": str(e)}, status=500)


async def handle_gps(request):
    """
    POST /api/v1/emulator/gps
    Updates the mock geolocation of the emulator.
    """
    if not EMULATOR_CHANNEL:
        return web.json_response({"error": "No channel to emulator"}, status=503)

    try:
        data = await request.json()
        latitude = float(data.get("latitude", 0.0))
        longitude = float(data.get("longitude", 0.0))
        altitude = float(data.get("altitude", 0.0))

        stub = ec_grpc.EmulatorControllerStub(EMULATOR_CHANNEL)
        metadata = get_emulator_metadata()

        # Send POSITION physical model update
        req = ec.PhysicalModelValue(
            target=ec.PhysicalModelValue.PhysicalType.POSITION,
            value=ec.ParameterValue(data=[longitude, latitude, altitude]),
        )
        await stub.setPhysicalModel(req, metadata=metadata)
        return web.json_response({"status": "success"})
    except Exception as e:
        logging.error(f"Error setting GPS on emulator: {e}")
        return web.json_response({"error": str(e)}, status=500)


async def handle_logcat(request):
    """
    GET /api/v1/emulator/logcat
    Fetches a chunk of logs from logcat using polling-friendly start/next structure.
    """
    if not EMULATOR_CHANNEL:
        return web.json_response({"error": "No channel to emulator"}, status=503)

    try:
        start_offset = int(request.query.get("start", "0"))
        stub = ec_grpc.EmulatorControllerStub(EMULATOR_CHANNEL)
        metadata = get_emulator_metadata()

        req = ec.LogMessage(start=start_offset)
        res = await stub.getLogcat(req, metadata=metadata)
        return web.json_response({"next": res.next, "contents": res.contents})
    except Exception as e:
        logging.error(f"Error fetching logcat from emulator: {e}")
        return web.json_response({"error": str(e)}, status=500)


def _ice_server_entries():
    turn_url = os.environ.get("TURN_URL", "").strip()
    if turn_url:
        return [{
            "urls": [turn_url],
            "username": os.environ.get("TURN_USERNAME", ""),
            "credential": os.environ.get("TURN_CREDENTIAL", ""),
        }]
    return [{"urls": ["stun:stun.l.google.com:19302"]}]


def _legacy_ice_config(entries):
    config = ice.IceServerConfig()
    for entry in entries:
        server = config.ice_servers.add()
        server.urls.extend(entry["urls"])
        if entry.get("username"):
            server.username = entry["username"]
        if entry.get("credential"):
            server.credential = entry["credential"]
    return config


def _new_ice_config(entries):
    config = rtc_new.IceServerConfig()
    config.ice_transport_policy = rtc_new.IceTransportPolicy.ICE_TRANSPORT_POLICY_ALL
    for entry in entries:
        server = config.ice_servers.add()
        server.urls.extend(entry["urls"])
        if entry.get("username"):
            server.username = entry["username"]
        if entry.get("credential"):
            server.credential = entry["credential"]
    return config


def _browser_signal_from_new(message):
    if message.HasField("sdp"):
        type_name = rtc_new.SessionDescription.Type.Name(message.sdp.type).lower()
        return {"sdp": {"type": type_name, "sdp": message.sdp.sdp}}
    if message.HasField("ice_candidate"):
        candidate = message.ice_candidate
        return {"candidate": {
            "candidate": candidate.candidate,
            "sdpMid": candidate.sdp_mid,
            "sdpMLineIndex": candidate.sdp_m_line_index,
            "usernameFragment": candidate.username_fragment or None,
        }}
    return None


def _new_message_from_browser(data, session):
    if "sdp" in data:
        desc = data["sdp"]
        type_value = {
            "offer": rtc_new.SessionDescription.OFFER,
            "answer": rtc_new.SessionDescription.ANSWER,
            "pranswer": rtc_new.SessionDescription.PRANSWER,
            "rollback": rtc_new.SessionDescription.ROLLBACK,
        }.get(str(desc.get("type", "")).lower(), rtc_new.SessionDescription.TYPE_UNSPECIFIED)
        return rtc_new.JsepMessage(
            handle=session,
            sdp=rtc_new.SessionDescription(type=type_value, sdp=desc.get("sdp", "")),
        )
    if "candidate" in data:
        candidate = data["candidate"]
        return rtc_new.JsepMessage(
            handle=session,
            ice_candidate=rtc_new.IceCandidate(
                candidate=candidate.get("candidate", ""),
                sdp_mid=candidate.get("sdpMid", ""),
                sdp_m_line_index=int(candidate.get("sdpMLineIndex", 0)),
                username_fragment=candidate.get("usernameFragment") or None,
            ),
        )
    return None


async def _handle_websocket_jsep_v2(ws, request, bridge_metadata):
    rtc_stub = rtc_new_grpc.RtcServiceStub(VIDEOBRIDGE_CHANNEL)
    session = None
    stream_task = None
    entries = _ice_server_entries()

    try:
        ice_config = _new_ice_config(entries)
        stream_req = rtc_new.RtcStreamRequest(
            requested_tracks=[rtc_new.TrackType.TRACK_TYPE_SCREEN_PRIMARY],
            ice_server_config=ice_config,
        )
        response = await rtc_stub.RequestRtcStream(stream_req, metadata=bridge_metadata)
        session = response.handle
        guid = session.session_id
        logging.info(
            "Initialized Android Emulation v2 RTC session %s, tracks=%s",
            guid,
            [(binding.mid, rtc_new.TrackType.Name(binding.track_type), binding.display_id) for binding in response.track_bindings],
        )

        server_ice = []
        for server in response.ice_servers:
            entry = {"urls": list(server.urls)}
            if server.username:
                entry["username"] = server.username
            if server.credential:
                entry["credential"] = server.credential
            server_ice.append(entry)
        start_msg = {"start": {"iceServers": server_ice or entries}}
        logging.info("Sending 'start' signal to client: %s", start_msg)
        await ws.send_json(start_msg)

        async def stream_to_client():
            try:
                async for message in rtc_stub.ReceiveJsepMessageStream(session, metadata=bridge_metadata):
                    signal = _browser_signal_from_new(message)
                    if signal is None:
                        logging.warning("Ignoring unsupported JSEP message: %s", message)
                        continue
                    logging.info("Forwarding JSEP message to browser: %s", signal)
                    await ws.send_json(signal)
            except asyncio.CancelledError:
                logging.info("stream_to_client task cancelled for connection: %s", guid)
            except Exception:
                logging.exception("Error in v2 RTC message stream")

        stream_task = asyncio.create_task(stream_to_client())

        async for ws_msg in ws:
            if ws_msg.type == web.WSMsgType.TEXT:
                data = ws_msg.json()
                logging.info("Received message from Client -> v2 RTC: %s", data)
                if "bye" in data:
                    break
                message = _new_message_from_browser(data, session)
                if message is not None:
                    await rtc_stub.SendJsepMessage(message, metadata=bridge_metadata)
            elif ws_msg.type == web.WSMsgType.ERROR:
                logging.error("WebSocket encountered error: %s", ws.exception())
    except Exception:
        logging.exception("Error in v2 WebSocket JSEP handler")
    finally:
        if stream_task:
            stream_task.cancel()
            try:
                await stream_task
            except asyncio.CancelledError:
                pass
        if session:
            try:
                await rtc_stub.CloseRtcStream(
                    rtc_new.CloseRtcStreamRequest(handle=session),
                    metadata=bridge_metadata,
                )
            except Exception:
                logging.debug("Failed to close RTC session", exc_info=True)
        await ws.close()


async def _handle_websocket_jsep_legacy(ws, request, bridge_metadata):
    rtc_stub = rtc_grpc.RtcStub(VIDEOBRIDGE_CHANNEL)
    guid = None
    stream_task = None

    try:
        entries = _ice_server_entries()
        ice_config = _legacy_ice_config(entries)
        stream_req = rtc.RtcStreamRequest(ice_server_config=ice_config)
        response = await rtc_stub.RequestRtcStream(stream_req, metadata=bridge_metadata)
        guid = response.id.guid
        logging.info("Initialized legacy WebRTC stream session: %s", guid)

        start_msg = {"start": {"iceServers": entries}}
        logging.info("Sending 'start' signal to client: %s", start_msg)
        await ws.send_json(start_msg)

        async def stream_to_client():
            try:
                reader_req = rtc.ReceiveJsepMessageRequest(id=rtc.Id(guid=guid))
                async for response_msg in rtc_stub.ReceiveJsepMessageStream(
                    reader_req, metadata=bridge_metadata
                ):
                    msg_text = response_msg.jsep_msg.message
                    if not msg_text:
                        break
                    await ws.send_json(json.loads(msg_text))
            except asyncio.CancelledError:
                logging.info("stream_to_client task cancelled for connection: %s", guid)
            except Exception:
                logging.exception("Error in legacy RTC message stream")

        stream_task = asyncio.create_task(stream_to_client())

        async for ws_msg in ws:
            if ws_msg.type == web.WSMsgType.TEXT:
                client_data = ws_msg.json()
                logging.info("Received message from Client -> legacy RTC: %s", client_data)
                send_req = rtc.SendJsepMessageRequest()
                send_req.jsep_msg.id.guid = guid
                send_req.jsep_msg.message = json.dumps(client_data)
                await rtc_stub.SendJsepMessage(send_req, metadata=bridge_metadata)
                if "bye" in client_data:
                    break
            elif ws_msg.type == web.WSMsgType.ERROR:
                logging.error("WebSocket encountered error: %s", ws.exception())
    except Exception:
        logging.exception("Error in legacy WebSocket JSEP handler")
    finally:
        if stream_task:
            stream_task.cancel()
            try:
                await stream_task
            except asyncio.CancelledError:
                pass
        await ws.close()


async def handle_websocket_jsep(request):
    ws = web.WebSocketResponse()
    await ws.prepare(request)
    logging.info("WebSocket connection opened for JSEP signaling.")

    if not VIDEOBRIDGE_CHANNEL:
        logging.error("No channel to Video Bridge.")
        await ws.close(code=web.WSCloseCode.INTERNAL_ERROR, message=b"Video Bridge offline")
        return ws

    bridge_metadata = (
        get_videobridge_metadata(request.app["videobridge_token"])
        if request.app["videobridge_token"]
        else get_emulator_metadata()
    )
    rtc_api = os.environ.get("RTC_API", "legacy").strip().lower()
    if rtc_api == "v2":
        await _handle_websocket_jsep_v2(ws, request, bridge_metadata)
    else:
        await _handle_websocket_jsep_legacy(ws, request, bridge_metadata)
    return ws


@web.middleware
async def cors_middleware(request, handler):
    if request.method == "OPTIONS":
        response = web.Response(status=200)
    else:
        response = await handler(request)

    response.headers["Access-Control-Allow-Origin"] = "*"
    response.headers["Access-Control-Allow-Methods"] = "GET, POST, OPTIONS, PUT, DELETE"
    response.headers["Access-Control-Allow-Headers"] = "Content-Type, Authorization"
    return response


def init_app(videobridge_token):
    app = web.Application(middlewares=[cors_middleware])
    app["videobridge_token"] = videobridge_token

    # Register routes
    app.router.add_get("/api/v1/emulator/status", handle_status)
    app.router.add_post("/api/v1/emulator/gps", handle_gps)
    app.router.add_get("/api/v1/emulator/logcat", handle_logcat)
    app.router.add_get("/api/v1/emulator/ws-jsep", handle_websocket_jsep)

    return app


async def main():
    parser = argparse.ArgumentParser(
        description="Python Web Gateway for WebRTC Android Emulator"
    )
    parser.add_argument(
        "--port", type=int, default=8080, help="Port to run the gateway webserver on."
    )
    parser.add_argument(
        "--videobridge",
        type=str,
        default="",
        help="Address of the running videobridge gRPC service. If empty, connects directly to the emulator gRPC service.",
    )
    parser.add_argument(
        "--videobridge_token",
        type=str,
        default="",
        help="Auth token for the videobridge.",
    )
    parser.add_argument(
        "--discovery_file",
        type=str,
        required=True,
        help="Path to the active emulator discovery config (.ini) file.",
    )
    args = parser.parse_args()

    # Parse discovery configuration properties
    global DISCOVERY_PROPS
    DISCOVERY_PROPS = parse_discovery_file(args.discovery_file)

    emulator_port = DISCOVERY_PROPS.get("grpc.port")
    if not emulator_port:
        logging.error("Failed to discover emulator gRPC port from discovery file.")
        sys.exit(1)

    emulator_address = f"localhost:{emulator_port}"
    logging.info(f"Connecting to Emulator gRPC service at: {emulator_address}")

    global EMULATOR_CHANNEL, VIDEOBRIDGE_CHANNEL
    EMULATOR_CHANNEL = grpc.aio.insecure_channel(emulator_address)

    if args.videobridge:
        logging.info(f"Connecting to Video Bridge gRPC service at: {args.videobridge}")
        VIDEOBRIDGE_CHANNEL = grpc.aio.insecure_channel(args.videobridge)
    else:
        logging.info("No separate Video Bridge specified; using Emulator gRPC channel directly for Rtc service.")
        VIDEOBRIDGE_CHANNEL = EMULATOR_CHANNEL

    # Start Aiohttp server
    app = init_app(args.videobridge_token)
    runner = web.AppRunner(app)
    await runner.setup()
    site = web.TCPSite(runner, "0.0.0.0", args.port)
    await site.start()

    logging.info(f"Gateway Webserver listening on http://0.0.0.0:{args.port}")

    # Run forever
    try:
        await asyncio.Event().wait()
    except (KeyboardInterrupt, SystemExit, asyncio.CancelledError):
        pass
    finally:
        logging.info("Shutting down Gateway channels.")
        await EMULATOR_CHANNEL.close()
        if VIDEOBRIDGE_CHANNEL and VIDEOBRIDGE_CHANNEL != EMULATOR_CHANNEL:
            await VIDEOBRIDGE_CHANNEL.close()
        await runner.cleanup()


def run_main():
    asyncio.run(main())


if __name__ == "__main__":
    run_main()
