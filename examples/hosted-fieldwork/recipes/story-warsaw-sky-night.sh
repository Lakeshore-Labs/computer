python3 - <<"PY"
from tools import ks,tm
import json,math,datetime
J=lambda x: json.loads(x) if isinstance(x,str) else x
today=datetime.date.today()
print("run date (host clock, UTC):",today)
print("legend: [provider] = data returned by a hosted MCP server; [own arithmetic] = computed here in WASI Python")
sky=J(ks.tonight_sky(city="Warsaw",lang="en"))
night_start=datetime.datetime.fromisoformat(sky["night"]["from"])
offset=night_start.utcoffset()
if offset is None:
    raise ValueError("Provider must return a local night with a UTC offset")
local_hours=int(offset.total_seconds() // 3600)
astro=J(tm.get_astronomy(date=str(today),lat=52.2297,lon=21.0122,tz=local_hours))
show=J(ks.meteor_showers(lang="en"))
print("[provider] kosmos verdict:",sky["verdict"],"|",sky["verdict_text"][:80])
print("[provider] kosmos night window:",sky["night"]["from"][11:16],"->",sky["night"]["until"][11:16],"(Polish time)")
moon=sky["moon"]; print("[provider] kosmos moon:",moon["phase"],moon["illuminated_percent"],"% illuminated")
sun=astro["sun"]; print("[provider] time.memoryapi.org sun (tool text claims USNO; not verified here): sunset",sun["set"],"| civil twilight ends",sun["civil_twilight_end"])
# independent: NOAA sunset
lat,lon=52.2297,21.0122; n=today.timetuple().tm_yday; g=2*math.pi/365*(n-1)
eqt=229.18*(0.000075+0.001868*math.cos(g)-0.032077*math.sin(g)-0.014615*math.cos(2*g)-0.040849*math.sin(2*g))
dec=0.006918-0.399912*math.cos(g)+0.070257*math.sin(g)-0.006758*math.cos(2*g)+0.000907*math.sin(2*g)-0.002697*math.cos(3*g)+0.00148*math.sin(3*g)
ha=math.degrees(math.acos(math.cos(math.radians(90.833))/(math.cos(math.radians(lat))*math.cos(dec))-math.tan(math.radians(lat))*math.tan(dec)))
ss=720-4*(lon-ha)-eqt+60*local_hours
mins=lambda s:int(s[:2])*60+int(s[3:5])
sm=round(ss)  # nearest minute, not truncation
print("[own arithmetic] NOAA-style solar equations, sunset to nearest minute: %02d:%02d (%.2f min after midnight)"%(sm//60,sm%60,ss),"| provider minus own:",mins(sun["set"])-sm,"min")
# independent: moon illumination from a mean synodic-month model (new moon 2000-01-06 18:14 UTC)
ref=datetime.datetime(2000,1,6,18,14); now=datetime.datetime.combine(today,datetime.time(21,0))-datetime.timedelta(hours=local_hours)
age=((now-ref).total_seconds()/86400)%29.530588853
ill=(1-math.cos(2*math.pi*age/29.530588853))/2*100
print("[own arithmetic] synodic model: moon age %.1f d, %.0f%% illuminated | kosmos says %s%%"%(age,ill,moon["illuminated_percent"]))
gem=[s for s in show["showers"] if s["code"]=="GEM"][0]
pk=datetime.date.fromisoformat(gem["peak"][:10]); days=(pk-today).days
age2=((datetime.datetime.combine(pk,datetime.time(1,0))-ref).total_seconds()/86400)%29.530588853
ill2=(1-math.cos(2*math.pi*age2/29.530588853))/2*100
print("[provider] Geminids peak",pk,"in",days,"days | [own arithmetic] model moon then: %.0f%% illuminated"%ill2)
print("CHECK sunset agrees within 5 min:",abs(mins(sun["set"])-sm)<=5)
print("CHECK dark window starts after civil twilight:",mins(sky["night"]["from"][11:16])>=mins(sun["civil_twilight_end"]))
print("CHECK kosmos moon within 6 points of model:",abs(ill-moon["illuminated_percent"])<=6)
print("CHECK Geminids peak is Dec 13/14:",pk.month==12 and pk.day in (13,14))
PY
