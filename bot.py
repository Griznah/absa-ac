import asyncio
import logging
import os

import aiohttp
import discord
from discord.ext import commands, tasks
from dotenv import load_dotenv

load_dotenv()

# ================= PYTHON 3.12+ FIX =================
asyncio.set_event_loop(asyncio.new_event_loop())

# ================= CONFIG =================

DISCORD_TOKEN = os.getenv("DISCORD_TOKEN")
CHANNEL_ID = int(os.getenv("CHANNEL_ID"))
SERVER_IP = os.getenv("SERVER_IP")
UPDATE_INTERVAL = 30  # seconds

SERVERS = [
    # -------- DRIFT --------
    {
        "name": "ABSA Drift#1 | Rotating Maps | BDC 4.0",
        "ip": SERVER_IP,
        "port": 8081,
        "category": "Drift",
    },
    {
        "name": "ABSA Drift#2 | Rotating Maps | Gravy Garage",
        "ip": SERVER_IP,
        "port": 8082,
        "category": "Drift",
    },
    {
        "name": "ABSA Drift#3 | Rotating Maps | SWARM 3.2",
        "ip": SERVER_IP,
        "port": 8083,
        "category": "Drift",
    },
    {
        "name": "ABSA Drift#4 | Rotating Maps | SWARM 3.2",
        "ip": SERVER_IP,
        "port": 8084,
        "category": "Drift",
    },
    {
        "name": "ABSA Drift#8 | Rotating Maps | SWARM 3.2 Touge",
        "ip": SERVER_IP,
        "port": 8088,
        "category": "Drift",
    },
    # -------- TOUGE --------
    {
        "name": "ABSA Race#6 | Touge FAST Lap",
        "ip": SERVER_IP,
        "port": 8086,
        "category": "Touge",
    },
    # -------- TRACK --------
    {
        "name": "ABSA Race#5 | Nordschleife Tourist FAST Lap",
        "ip": SERVER_IP,
        "port": 8085,
        "category": "Track",
    },
    {
        "name": "ABSA GoKart#7 | Rotating Maps |",
        "ip": SERVER_IP,
        "port": 8087,
        "category": "Track",
    },
    {
        "name": "ABSA SRP#9 | SRP Traffic|",
        "ip": SERVER_IP,
        "port": 8089,
        "category": "Track",
    },
    {
        "name": "ABSA SRP#10 | SRP Traffic|",
        "ip": SERVER_IP,
        "port": 8090,
        "category": "Track",
    },
]

CATEGORY_ORDER = ["Drift", "Touge", "Track"]

CATEGORY_EMOJIS = {
    "Drift": "🟣",
    "Touge": "🟠",
    "Track": "🔵",
}

# ================= LOGGING =================

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger(__name__)

# ================= BOT SETUP =================

intents = discord.Intents.default()
bot = commands.Bot(command_prefix="!", intents=intents)

server_message = None

# ================= SERVER FETCH =================


async def get_server_info(session, server):
    try:
        url = f"http://{server['ip']}:{server['port']}/info"
        async with session.get(url, timeout=2) as response:
            data = await response.json()

            num_players = data.get("clients", 0)
            max_players = data.get("maxclients", 0)
            track_name = os.path.basename(data.get("track", "Unknown"))

            return {
                "name": server["name"],
                "category": server["category"],
                "map": track_name,
                "players": f"{num_players}/{max_players}",
                "num_players": num_players,
                "ip": server["ip"],
                "port": server["port"],
            }
    except:
        return {
            "name": server["name"],
            "category": server["category"],
            "map": "Offline",
            "players": "0/0",
            "num_players": -1,
            "ip": server["ip"],
            "port": server["port"],
        }


# ================= EMBED BUILDER =================


async def build_embed():
    async with aiohttp.ClientSession() as session:
        server_infos = await asyncio.gather(*(get_server_info(session, s) for s in SERVERS))

    # Group servers + totals
    grouped = {cat: [] for cat in CATEGORY_ORDER}
    category_totals = {cat: 0 for cat in CATEGORY_ORDER}
    total_players = 0

    for info in server_infos:
        grouped.setdefault(info["category"], []).append(info)
        if info["num_players"] > 0:
            category_totals[info["category"]] += info["num_players"]
            total_players += info["num_players"]

    embed = discord.Embed(
        title="ABSA Official Servers",
        description=f"👥 **Total Players:** {total_players}",
        color=discord.Color.green(),
    )

    embed.set_thumbnail(
        url="https://upload.wikimedia.org/wikipedia/commons/thumb/d/d9/Flag_of_Norway.svg/320px-Flag_of_Norway.svg.png"
    )

    for category in CATEGORY_ORDER:
        emoji = CATEGORY_EMOJIS.get(category, "📁")
        total = category_totals.get(category, 0)

        # Category header
        embed.add_field(
            name=f"{emoji} **{category} Servers — {total} players**",
            value="\u200b",
            inline=False,
        )

        for info in grouped.get(category, []):
            status_emoji = "🟢" if info["num_players"] >= 0 else "🔴"

            # Server field with "Join Server" link
            join_url = (
                f"https://acstuff.club/s/q:race/online/join?ip={info['ip']}&httpPort={info['port']}"
            )
            embed.add_field(
                name=f"{status_emoji} {info['name']}",
                value=f"**Map:** {info['map']}\n**Players:** {info['players']}\n[Join Server]({join_url})",
                inline=False,
            )

        # Extra spacing after category
        embed.add_field(name="\u200b", value="\u200b", inline=False)

    embed.set_image(url=f"http://{SERVER_IP}/images/logo.png")
    embed.set_footer(text=f"Updates every {UPDATE_INTERVAL} seconds")

    return embed


# ================= TASK LOOP =================


@tasks.loop(seconds=UPDATE_INTERVAL)
async def update_servers():
    global server_message

    channel = bot.get_channel(CHANNEL_ID)
    if channel is None:
        logger.error(f"Channel ID {CHANNEL_ID} not found")
        return

    embed = await build_embed()

    try:
        if server_message is None:
            server_message = await channel.send(embed=embed)
            logger.info("Initial status message posted")
        else:
            await server_message.edit(embed=embed)
            logger.info("Status message updated")
    except discord.NotFound:
        server_message = await channel.send(embed=embed)
        logger.info("Status message recreated (previous message was deleted)")


# ================= EVENTS =================


@bot.event
async def on_ready():
    logger.info(f"✅ Logged in as {bot.user}")
    update_servers.start()


# ================= RUN =================


async def main():
    logger.info("Starting bot...")
    async with bot:
        await bot.start(DISCORD_TOKEN)


if not DISCORD_TOKEN:
    raise RuntimeError("DISCORD_TOKEN environment variable not set")
if not CHANNEL_ID:
    raise RuntimeError("CHANNEL_ID environment variable not set")
if not SERVER_IP:
    raise RuntimeError("SERVER_IP environment variable not set")

asyncio.run(main())
