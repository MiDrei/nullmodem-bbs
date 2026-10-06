#!/usr/bin/env python3
"""Generates the stock space-themed screens into configs/screens: the
main menu, its submenus, the sysop menu and the logoff screen, each as
name.ans (English text) and name.de.ans ({T:key} placeholders).

    python3 scripts/screens/space.py [OUTDIR/]

Look at the result with scripts/screens/preview (see README.md) before
committing it."""
import math, os, random, re, json, sys
ROOT=os.path.join(os.path.dirname(os.path.abspath(__file__)),'..','..')+'/'
OUT=sys.argv[1] if len(sys.argv)>1 else ROOT+'configs/screens/'
EN={}
for l in open(ROOT+'internal/i18n/lang/en.yaml',encoding='utf-8'):
    m=re.match(r'^([a-z0-9_.-]+): (".*")\s*$',l)
    if m: EN[m.group(1)]=json.loads(m.group(2))
ESC='\x1b['
W=79  # the board lays screens out one column short of 80 (wrapMargin)
def sgr(fg,bg=0): return f"{ESC}{1 if fg>7 else 0};{30+fg%8};{40+bg}m"
DARK={8:0,9:1,10:2,11:3,12:4,13:5,14:6,15:7}

# ---------------- header art ----------------
class Art:
    def __init__(s,rows,seed):
        s.R=rows; s.cells=[[(' ',7,0) for _ in range(W)] for _ in range(rows)]
        s.pix=[[None]*W for _ in range(rows*2)]; s.rnd=random.Random(seed)
    def nebula(s,pal,yc0,slope,amp,width,x0,x1,seed=3,rows=None):
        rnd=random.Random(seed)
        for y in range(rows or s.R):
            for x in range(W):
                yc=yc0-x*slope+amp*math.sin(x/9.0)
                band=max(0.0,1-abs(y-yc)/(width+0.5*math.sin(x/5.0+1)))
                fade=max(0.0,min(1.0,(x-x0)/8.0))*max(0.0,min(1.0,(x1-x)/16.0))
                n=0.5+0.25*math.sin(x*0.41+y*1.7)+0.25*math.sin(x*0.17-y*0.9+2.0)
                d=band*fade*(0.5+0.55*n)+rnd.uniform(-0.12,0.12)
                for lim,(ch,fg) in zip((0.80,0.66,0.52,0.38,0.24),reversed(pal)):
                    if d>lim: s.cells[y][x]=(ch,fg,0); break
    def stars(s,n,bright=()):
        for _ in range(n):
            x,y=s.rnd.randrange(W),s.rnd.randrange(s.R)
            ch,fg=s.rnd.choice([('.',8),('.',8),('·',8),('·',7),('∙',7),('.',7),('*',15),('+',8)])
            if s.cells[y][x][0]==' ': s.cells[y][x]=(ch,fg,0)
        for (x,y,ch) in bright: s.cells[y][x]=(ch,15,0)
    def put(s,x,y,items):
        for i,(ch,fg) in enumerate(items): s.cells[y][x+i]=(ch,fg,0)
    def disc(s,cx,cy,r,shades,light=(-0.8,-0.6)):
        for y in range(s.R*2):
            for x in range(W):
                dx,dy=x+0.5-cx,y+0.5-cy; d=math.hypot(dx,dy)
                if d<=r:
                    dot=(dx*light[0]+dy*light[1])/r
                    s.pix[y][x]=shades[0] if dot>0.35 else (shades[1] if dot>-0.35 else shades[2])
    def planet_edge(s,cx,cy,r):
        for y in range(s.R*2):
            for x in range(W):
                d=math.hypot(x+0.5-cx,y+0.5-cy)
                if d<=r: s.pix[y][x]=14 if d>r-1.1 else (6 if d>r-2.3 else 4)
    def lines(s):
        for row in range(s.R):
            for x in range(W):
                t,b=s.pix[2*row][x],s.pix[2*row+1][x]
                if t is None and b is None: continue
                t=t or 0; b=b or 0
                if t==b: c=('█',t,0) if t>7 else (' ',7,t)
                elif b<8: c=('▀',t,b)
                elif t<8: c=('▄',b,t)
                else: c=('▀',t,DARK[b])
                s.cells[row][x]=c
        out=[]
        for row in s.cells:
            line=''; cur=None
            while row and row[-1]==(' ',7,0): row=row[:-1]
            for ch,fg,bg in row:
                if ch==' ' and bg==0 and cur and cur[1]==0: line+=' '; continue
                if (fg,bg)!=cur: line+=sgr(fg,bg); cur=(fg,bg)
                line+=ch
            # a background colour must not run on into the next line
            out.append(line+(ESC+'0m' if cur and cur[1] else ''))
        return out

VIOLET=[('░',4),('░',5),('▒',5),('░',13),('▒',13)]
BLUE=[('░',4),('▒',4),('░',6),('░',14),('▒',14)]
GREEN=[('░',2),('▒',2),('░',6),('░',10),('▒',10)]
WARM=[('░',5),('░',1),('▒',1),('░',9),('▒',13)]
RED=[('░',8),('░',1),('▒',1),('░',9),('▒',9)]

def satellite(a,x,y):
    a.put(x,y,[('▓',12),('▓',12),('─',8),('█',7),('─',8),('▓',12),('▓',12)])
    a.cells[y-1][x+3]=('│',8,0)

def head_main(seed=7):
    a=Art(6,seed)
    a.nebula(VIOLET,3.0,0.03,1.0,1.7,0,58)
    a.stars(40,[(14,3,'*'),(33,1,'∙'),(41,2,'+'),(24,4,'∙'),(52,0,'*')])
    a.planet_edge(48+32+4.0,-34.0,42.0)
    a.disc(48+6.0,9.0,1.0,(15,7,7))
    satellite(a,64,4)
    return a.lines()
def head_messages():
    a=Art(6,11)
    a.nebula(BLUE,2.7,0.02,0.9,1.7,0,52,seed=5,rows=5)
    a.stars(38,[(9,1,'*'),(30,3,'+'),(16,5,'*')])
    satellite(a,62,3)
    a.put(70,2,[(')',6),(' ',0),(')',14)]); a.put(70,3,[(')',6),(' ',0),(')',14),(' ',0),(')',15)]); a.put(70,4,[(')',6),(' ',0),(')',14)])
    return a.lines()
def head_files():
    a=Art(6,23)
    a.nebula(GREEN,2.1,0.0,0.9,1.7,4,50,seed=9,rows=5)
    a.stars(38,[(20,3,'*'),(44,0,'+'),(37,5,'*')])
    a.disc(66.0,5.5,3.0,(7,8,8),light=(-0.7,-0.7))
    a.disc(74.0,2.5,1.2,(7,8,8),light=(-0.7,-0.7))
    for (x,y,ch,fg) in [(58,2,'\u25a0',8),(60,4,'\u2219',7),(71,4,'\u25a0',7),(78,4,'\u2219',8),(62,1,'\u2219',8)]:
        a.cells[y][x]=(ch,fg,0)
    return a.lines()
def head_community():
    a=Art(6,31)
    a.nebula(WARM,2.9,0.03,1.0,1.7,0,54,seed=13,rows=5)
    a.stars(38,[(12,0,'*'),(36,4,'+'),(29,5,'*')])
    a.disc(70.0,5.0,3.8,(13,5,4))
    a.put(52,1,[('\u00b7',8),('\u00b7',8),('-',8),('-',4),('\u2500',12),('\u2500',14),('*',15)])
    return a.lines()
def head_sysop():
    a=Art(6,41)
    a.nebula(RED,2.3,0.0,0.8,1.7,0,60,seed=17,rows=5)
    a.stars(34,[(70,1,'*'),(18,5,'*')])
    return a.lines()

# ---------------- frame + items ----------------
F=4
KEYS=None
def T(k,w=None,de=True):
    """Text k: a {T:key} placeholder for .de screens, the English text
    for the others; w pads it like {NAME:w} (negative: left-aligned)."""
    if de: return '{T:%s%s}'%(k,'' if w is None else ':%d'%w)
    v=EN[k]
    if w is None: return v
    return v[:-w].ljust(-w) if w<0 else v[:w].rjust(w)
def key(k): return sgr(8)+'['+sgr(14)+k+sgr(8)+']'
class Screen:
    def __init__(s,de): s.de=de; s.L=[]
    def t(s,k,w=None): return T(k,w,s.de)
    def top(s,right):
        s.L.append(sgr(F)+'┌─┤ '+sgr(15)+'{BBSNAME}'+sgr(F)+' ├{FILL:─}┤ '+sgr(14)+right+sgr(F)+' ├─┐')
    def row(s,content='',prefix=''):
        s.L.append(prefix+sgr(F)+'│'+sgr(7)+content+sgr(7)+'{FILL: }'+sgr(F)+'│')
    def item(s,k,label,desc=None,sub=False,prefix=''):
        c='  '+key(k)+' '+sgr(15)+s.t(label,-24 if desc else None)
        if desc: c+=sgr(12)+('» ' if sub else '  ')+sgr(7)+s.t(desc)
        s.row(c,prefix)
    def footer(s,items):
        s.row('  '+'     '.join(key(k)+' '+sgr(15)+s.t(l) for k,l in items))
    def center(s,text):
        s.L.append(sgr(F)+'│'+sgr(7)+'{FILL: }'+text+sgr(7)+'{FILL: }'+sgr(F)+'│')
    def bot(s): s.L.append(sgr(F)+'└{FILL:─}┘')
    def prompt(s): s.L+=['',sgr(15)+'{USERNAME}'+sgr(12)+' » '+ESC+'0m']
    def text(s,head):
        return (ESC+'2J'+ESC+'H'+'\r\n'.join(head+s.L)).encode('cp437')

MENUS={
 'main':(head_main,'common.main_menu',[
    ('R','menu.item.newscan','screen.desc.newscan',False),
    ('N','menu.item.news','screen.desc.news',False),None,
    ('M','menu.item.messages','screen.desc.messages',True),
    ('F','menu.item.files_menu','screen.desc.files',True),
    ('T','menu.item.community','screen.desc.community',True),
    ('D','menu.item.doors','screen.desc.doors',False),
    ('P','menu.item.profile','screen.desc.profile',False),
    ('S','menu.sysop_item','screen.desc.sysop',True,'{SYSOP_ONLY}')],
    [('?','menu.item.version'),('Q','menu.item.quit')]),
 'messages':(head_messages,'menu.item.messages',[
    ('R','menu.item.newscan','screen.desc.newscan',False),
    ('T','menu.item.tome','screen.desc.tome',False),
    ('A','menu.item.areas','screen.desc.areas',False),
    ('N','menu.item.netmail','screen.desc.netmail',False),
    ('I','menu.item.nodelist','screen.desc.nodelist',False),None,
    ('K','menu.item.myareas','screen.desc.myareas',False),
    ('O','menu.item.qwk_get','screen.desc.qwk_get',False),
    ('U','menu.item.qwk_put','screen.desc.qwk_put',False)],
    [('Q','menu.item.back')]),
 'files':(head_files,'menu.item.files_menu',[
    ('A','menu.item.files','screen.desc.fileareas',False),
    ('N','menu.item.newfiles','screen.desc.newfiles',False),
    ('S','menu.item.filesearch','screen.desc.filesearch',False)],
    [('Q','menu.item.back')]),
 'community':(head_community,'menu.item.community',[
    ('C','menu.item.chat','screen.desc.chat',False),
    ('P','menu.item.page','screen.desc.page',False),
    ('L','menu.item.oneliners','screen.desc.oneliners',False),
    ('W','menu.item.who','screen.desc.who',False),None,
    ('Z','menu.item.lastcallers','screen.desc.lastcallers',False),
    ('V','menu.item.polls','screen.desc.polls',False),
    ('B','menu.item.bbslist','screen.desc.bbslist',False)],
    [('Q','menu.item.back')]),
 'sysop':(head_sysop,None,[
    ('L','screen.sysop.listusers',None,False),
    ('S','screen.sysop.setsl',None,False),
    ('C','screen.sysop.createarea',None,False),
    ('F','screen.sysop.createfilearea',None,False),
    ('I','screen.sysop.importfile',None,False)],
    [('M','screen.sysop.back'),('Q','menu.item.quit')]),
}

def head_doors():
    """A wormhole on the right, pulling in a violet nebula."""
    a=Art(6,61)
    a.nebula(VIOLET,2.6,0.0,0.8,1.6,0,62,seed=29,rows=5)
    a.stars(38)
    cx,cy,R,asp,twist,arms=60.0,5.0,4.9,2.4,1.5,3
    for y in range(10):
        for x in range(W):
            dx=(x+0.5-cx)/asp; dy=y+0.5-cy
            r=math.hypot(dx,dy)/R
            if r>1.12: continue
            a.cells[y//2][x]=(' ',7,0)
            if r>1.0: continue
            s_=(math.atan2(dy,dx)/(2*math.pi))*arms-math.log(r+0.02)*twist
            f=s_-math.floor(s_)
            arm=f<0.42
            if r<0.16: c=15
            elif r<0.3: c=14 if arm else 6
            elif r<0.5: c=(14 if f<0.2 else 6) if arm else 4
            elif r<0.75: c=(12 if f<0.2 else 4) if arm else (5 if f>0.8 else 0)
            else: c=(13 if f<0.15 else 5) if arm else 0
            if c: a.pix[y][x]=c
    return a.lines()

def doors_screens():
    """The door list's banner (with the frame's top and a blank row),
    its row, the gap before B/Q and the frame's bottom."""
    def head(de):
        s=Screen(de); s.top(T('common.doors',None,de)); s.row()
        return (ESC+'2J'+ESC+'H'+'\r\n'.join(head_doors()+s.L)).encode('cp437')
    row=sgr(F)+'\u2502'+'  '+key('{KEY}')+' '+sgr(15)+'{DOOR}'+sgr(7)+'{FILL: }'+sgr(F)+'\u2502'
    gap=sgr(F)+'\u2502'+sgr(7)+'{FILL: }'+sgr(F)+'\u2502'
    foot=sgr(F)+'\u2514{FILL:\u2500}\u2518'+ESC+'0m'
    def bulletins(de):
        s=Screen(de); s.top(T('doors.bulletins',None,de)); s.row()
        return (ESC+'2J'+ESC+'H'+'\r\n'.join(head_doors()+s.L)).encode('cp437')
    return {'doors.ans':head(False),'doors.de.ans':head(True),
            'doorbulletins.ans':bulletins(False),'doorbulletins.de.ans':bulletins(True),
            'doors-row.ans':row.encode('cp437'),'doors-gap.ans':gap.encode('cp437'),'doors-footer.ans':foot.encode('cp437')}

def menu_screen(name,de):
    head,title,items,foot=MENUS[name]
    s=Screen(de)
    right=s.t(title) if title else s.t('common.sysop_menu')+sgr(8)+' · '+sgr(7)+s.t('screen.sysop.node')
    s.top(right); s.row()
    for it in items:
        if it is None: s.row(); continue
        s.item(it[0],it[1],it[2],it[3],it[4] if len(it)>4 else '')
    s.row(); s.footer(foot); s.bot(); s.prompt()
    return s.text(head())

def logoff_screen(de):
    s=Screen(de)
    s.top(s.t('screen.logoff.thanks'))
    s.row()
    s.center(sgr(15)+s.t('logoff.goodbye'))
    s.center(sgr(7)+s.t('screen.logoff.again'))
    s.row(); s.bot()
    return s.text(head_main())+(ESC+'0m').encode()

# ---------------- lists and views ----------------
# A list's header: a three-row strip of stars and the section's nebula,
# then a title line with the board's name and the list's title.
def strip(pal,seed,neb_seed,motif=None):
    a=Art(3,seed)
    a.nebula(pal,1.0,0.0,0.7,1.2,0,60,seed=neb_seed,rows=2)
    a.stars(24)
    if motif: motif(a)
    return a.lines()
def title_line(title):
    return sgr(F)+'\u2500\u2524 '+sgr(15)+'{BBSNAME}'+sgr(F)+' \u251c{FILL:\u2500}\u2524 '+sgr(14)+title+sgr(F)+' \u251c\u2500'+ESC+'0m'
def list_head(head,title):
    return (ESC+'2J'+ESC+'H'+'\r\n'.join(head+[title_line(title)])+'\r\n')
def rule(): return sgr(F)+'\u2500'*W+ESC+'0m'
def divider(var,fg=6): return sgr(F)+'\u2500\u2500 '+sgr(fg)+'{'+var+'}'+sgr(F)+' {FILL:\u2500}'+ESC+'0m'
SELECTED=ESC+'1;37;44m'

def lists():
    """name -> (en text, de text or None): the list screens and their parts."""
    out={}
    def both(name,fn):
        out[name]=(fn(False),fn(True))
    msghead=lambda: strip(BLUE,5,21,motif=lambda a: satellite(a,66,1))
    both('msgareas',lambda de: list_head(msghead(),T('common.message_areas',None,de)))
    both('msgareas-columns',lambda de: sgr(7)+T('common.new',-3,de)+' '+T('common.area',-53,de)+' '+T('col.total',6,de)+' '+T('common.new_2',6,de)+' '+T('col.yours',7,de)+ESC+'0m\r\n'+rule())
    out['msgareas-row']=(sgr(14)+'{NEWFLAG:-3} '+sgr(15)+'{AREANAME:-53} '+sgr(8)+'{TOTAL:6} '+sgr(14)+'{NEW:6} '+sgr(7)+'{YOURS:7}'+ESC+'0m',None)
    out['msgareas-row-selected']=(SELECTED+'{NEWFLAG:-3} {AREANAME:-53} {TOTAL:6} {NEW:6} {YOURS:7}'+ESC+'0m',None)
    out['msgareas-network']=(divider('NETWORK'),None)

    out['msglist']=(list_head(msghead(),'{AREANAME}'),None)
    both('msglist-columns',lambda de: sgr(7)+T('common.new',-3,de)+' '+T('common.subject',-39,de)+' '+T('common.from',-18,de)+' '+T('col.date',16,de)+ESC+'0m\r\n'+rule())
    out['msglist-row']=(sgr(14)+'{NEWFLAG:-3} '+sgr(15)+'{SUBJECT:-39} '+sgr(7)+'{FROM:-18} '+sgr(8)+'{DATE:16}'+ESC+'0m',None)
    out['msglist-row-selected']=(SELECTED+'{NEWFLAG:-3} {SUBJECT:-39} {FROM:-18} {DATE:16}'+ESC+'0m',None)

    both('msgread',lambda de: list_head(msghead(),sgr(14)+'{AREANAME}'+sgr(8)+' \u00b7 '+sgr(7)+T('screen.msg_pos',None,de)))
    both('msgread-meta',lambda de: sgr(6)+T('msg.from',-9,de)+sgr(15)+'{FROM:-40}'+sgr(6)+' '+T('msg.date',None,de)+' '+sgr(7)+'{DATE}\r\n'
        +sgr(6)+T('msg.to',-9,de)+sgr(15)+'{TO:-40}\r\n'+sgr(6)+T('msg.subject',-9,de)+sgr(14)+'{SUBJECT:-70}\r\n'+rule())
    out['msgread-footer']=(sgr(8)+'{SCROLLSTATUS}'+ESC+'0m\r\n'+sgr(7)+'{HINT}'+ESC+'0m',None)
    both('msgpost',lambda de: list_head(msghead(),sgr(14)+T('screen.msgpost',None,de)+sgr(8)+' \u00b7 '+sgr(7)+'{AREANAME}'))

    # Netmail is private mail, not echomail: violet, as it always was.
    def comet(a): a.put(58,1,[('\u00b7',8),('\u00b7',8),('-',8),('-',5),('\u2500',13),('\u2500',15),('*',15)])
    nethead=lambda: strip(VIOLET,7,33,motif=comet)
    both('netmail',lambda de: list_head(nethead(),T('common.netmail',None,de)))
    both('netmail-columns',lambda de: sgr(7)+T('common.new',-3,de)+' '+T('common.subject',-39,de)+' '+T('common.from',-18,de)+' '+T('col.date',16,de)+ESC+'0m\r\n'+rule())
    out['netmail-row']=(sgr(13)+'{NEWFLAG:-3} '+sgr(15)+'{SUBJECT:-39} '+sgr(7)+'{FROM:-18} '+sgr(8)+'{DATE:16}'+ESC+'0m',None)
    out['netmail-row-selected']=(ESC+'1;37;45m{NEWFLAG:-3} {SUBJECT:-39} {FROM:-18} {DATE:16}'+ESC+'0m',None)
    both('netread',lambda de: list_head(nethead(),sgr(14)+T('screen.netread',None,de)+sgr(8)+' \u00b7 '+sgr(7)+T('screen.msg_pos',None,de)))
    both('netread-meta',lambda de: sgr(5)+T('msg.from',-9,de)+sgr(15)+'{FROM:-40}'+sgr(5)+' '+T('msg.date',None,de)+' '+sgr(7)+'{DATE}\r\n'
        +sgr(5)+T('msg.to',-9,de)+sgr(15)+'{TO:-40}\r\n'+sgr(5)+T('msg.subject',-9,de)+sgr(13)+'{SUBJECT:-70}\r\n'+rule())
    out['netread-footer']=out['msgread-footer']
    both('netpost',lambda de: list_head(nethead(),T('netmail.compose_title',None,de))+'\r\n')

    # Files: green, with the files menu's asteroids.
    def rocks(a):
        a.disc(66.0,3.0,2.7,(7,8,8),light=(-0.7,-0.7)); a.disc(72.5,1.5,1.0,(7,8,8))
        for (x,y,ch,fg) in [(60,0,'\u25a0',8),(71,1,'\u25a0',7),(74,2,'\u2219',8),(62,2,'\u2219',7)]: a.cells[y][x]=(ch,fg,0)
    filhead=lambda: strip(GREEN,23,9,motif=rocks)
    GSEL=ESC+'1;37;42m'
    both('filareas',lambda de: list_head(filhead(),T('common.file_areas',None,de)))
    both('filareas-columns',lambda de: sgr(7)+T('common.new',-3,de)+' '+T('common.area',-53,de)+' '+T('col.total',6,de)+' '+T('common.new_2',6,de)+' '+T('col.yours',7,de)+ESC+'0m\r\n'+rule())
    out['filareas-row']=(sgr(10)+'{NEWFLAG:-3} '+sgr(15)+'{AREANAME:-53} '+sgr(8)+'{TOTAL:6} '+sgr(10)+'{NEW:6} '+sgr(7)+'{YOURS:7}'+ESC+'0m',None)
    out['filareas-row-selected']=(GSEL+'{NEWFLAG:-3} {AREANAME:-53} {TOTAL:6} {NEW:6} {YOURS:7}'+ESC+'0m',None)
    out['filareas-network']=(divider('NETWORK',2),None)
    out['fillist']=(list_head(filhead(),'{AREANAME}'),None)
    both('fillist-columns',lambda de: sgr(7)+T('common.new',-3,de)+' '+T('common.filename',-30,de)+' '+T('col.by',-16,de)+' '+T('common.size',10,de)+' '+T('col.date',16,de)+ESC+'0m\r\n'+rule())
    out['fillist-row']=(sgr(10)+'{NEWFLAG:-3} '+sgr(15)+'{FILENAME:-30} '+sgr(7)+'{BY:-16} '+sgr(2)+'{SIZE:10} '+sgr(8)+'{DATE:16}'+ESC+'0m',None)
    out['fillist-row-selected']=(GSEL+'{NEWFLAG:-3} {FILENAME:-30} {BY:-16} {SIZE:10} {DATE:16}'+ESC+'0m',None)
    both('filread',lambda de: list_head(filhead(),sgr(14)+'{AREANAME}'+sgr(8)+' \u00b7 '+sgr(7)+T('screen.filread_pos',None,de)))
    both('filread-meta',lambda de: sgr(2)+T('files.filename',-11,de)+sgr(15)+'{FILENAME:-40}'+sgr(2)+' '+T('files.size',None,de)+' '+sgr(7)+'{SIZE}\r\n'
        +sgr(2)+T('files.uploaded',-11,de)+sgr(7)+'{DATE}'+sgr(2)+' '+T('files.by',None,de)+' '+sgr(15)+'{BY}\r\n'
        +sgr(2)+T('files.downloads',-11,de)+sgr(7)+'{DOWNLOADS}\r\n'+rule())
    out['filread-footer']=out['msgread-footer']

    # The features' banners: their menu's art, a title line and a blank
    # line; what follows comes from the board itself.
    def feature(head,title,de): return list_head(head(),T(title,None,de))+'\r\n'
    for name,title in (('who','common.who_s_online'),('lastcallers','common.interbbs_last_callers'),
                       ('oneliners','common.one_liners'),('polls','common.voting_booth'),('bbslist','common.bbs_list')):
        both(name,lambda de,title=title: feature(head_community,title,de))
    for name,title in (('profile','common.your_profile'),('summary','summary.title'),('news','news.title')):
        both(name,lambda de,title=title: feature(head_main,title,de))
    for name,title in (('sysusers','screen.sysop.listusers'),('syssetsl','screen.sysop.setsl'),('sysarea','screen.sysop.createarea'),
                       ('sysfilearea','screen.sysop.createfilearea'),('sysimport','screen.sysop.importfile')):
        both(name,lambda de,title=title: feature(head_sysop,title,de))
    for name,title in (('nodelist','common.nodelists'),('qwkget','menu.item.qwk_get'),('qwkput','menu.item.qwk_put')):
        both(name,lambda de,title=title: feature(head_messages,title,de))
    return out

# ---------------- welcome ----------------
# The board's own connect banner: a ringed planet, the name in bold
# letters with a blue shadow, the sysop's details and networks.
WELCOME_NAME=('MAIKS','PLACE')
WELCOME_INFO=[[('Sysop','Mike Dreier'),('Location','Neunkirch, CH')],
              [('Telnet','bbs.maik.ch:2323'),('E-Mail','maiks.place.bbs@relay.maik.ch')]]
WELCOME_NETS=[[('fsxNet','21:3/194'),('HobbyNet','954:700/14'),('LovlyNet','227:1/23')],
              [('tqwNet','1337:1/131'),('SysopNet','23:1/107')]]
BOLD={
'M':["##...##","###.###","##.#.##","##...##","##...##","##...##","##...##"],
'A':[".####.","##..##","##..##","######","##..##","##..##","##..##"],
'I':["##","##","##","##","##","##","##"],
'K':["##..##","##.##.","####..","###...","####..","##.##.","##..##"],
'S':[".#####","##....","##....",".####.","....##","....##","#####."],
'P':["#####.","##..##","##..##","#####.","##....","##....","##...."],
'L':["##....","##....","##....","##....","##....","##....","######"],
'C':[".#####","##....","##....","##....","##....","##....",".#####"],
'E':["######","##....","##....","#####.","##....","##....","######"],
}
def bold_text(a,text,cx,y0,grad,shadow):
    x=cx-(sum(len(BOLD[c][0])+1 for c in text)-1)//2
    pts=[]
    for ch in text:
        for yy,row in enumerate(BOLD[ch]):
            for xx,c in enumerate(row):
                if c=='#': pts.append((x+xx,y0+yy,yy))
        x+=len(BOLD[ch][0])+1
    for (px,py,_) in pts:
        if py+1<len(a.pix) and a.pix[py+1][px+1] is None: a.pix[py+1][px+1]=shadow
    for (px,py,yy) in pts: a.pix[py][px]=grad[yy]
def head_welcome():
    a=Art(9,13)
    a.stars(40,[(4,0,'*'),(74,7,'*')])
    for y in range(9):
        for x in range(27): a.cells[y][x]=(' ',7,0)
        if 0<y<8:
            for x in range(30,W): a.cells[y][x]=(' ',7,0)
    # the ringed planet; the ring passes in front of it at the bottom
    cx,cy,r=13.0,9.0,6.5
    for y in range(18):
        for x in range(28):
            dx,dy=x+0.5-cx,y+0.5-cy; d=math.hypot(dx,dy)
            rx,ry=(dx*0.96+dy*0.28),(-dx*0.28+dy*0.96)
            e=(rx/12.8)**2+(ry/3.3)**2; ring=0.62<e<1.0
            if d<=r:
                dot=(-dx*0.7-dy*0.7)/r
                c=13 if dot>0.4 else (5 if dot>-0.3 else 4)
                if ring and ry>0: c=14 if rx<0 else 6
                a.pix[y][x]=c
            elif ring: a.pix[y][x]=14 if rx<0 else 6
    for text,y in zip(WELCOME_NAME,(1,10)):
        bold_text(a,text,54,y,[15,15,14,14,14,6,6],4)
    return a.lines()
def welcome_screen():
    dot=sgr(8)+' \u2219 '
    def center(t): return sgr(F)+'\u2502{FILL: }'+t+sgr(7)+'{FILL: }'+sgr(F)+'\u2502'
    L=head_welcome()+['','{FILL: }'+sgr(8)+'.: '+sgr(7)+'Bulletin Board System'+sgr(8)+' :.{FILL: }','']
    L.append(sgr(F)+'\u250c{FILL:\u2500}\u2510')
    for row in WELCOME_INFO:
        L.append(center('     '.join(sgr(6)+k+dot+sgr(15)+v for k,v in row)))
    L.append(sgr(F)+'\u251c{FILL:\u2500}\u2524')
    for row in WELCOME_NETS:
        L.append(center(dot.join(sgr(14)+n+' '+sgr(7)+a for n,a in row)))
    L.append(sgr(F)+'\u2514{FILL:\u2500}\u2518')
    L.append(sgr(8)+'{FILL: }{VERSION} '+ESC+'0m')
    return (ESC+'2J'+ESC+'H'+'\r\n'.join(L)).encode('cp437')

def main():
    for name in MENUS:
        for de in (False,True):
            open(OUT+name+('.de' if de else '')+'.ans','wb').write(menu_screen(name,de))
    for de in (False,True):
        open(OUT+'logoff'+('.de' if de else '')+'.ans','wb').write(logoff_screen(de))
    open(OUT+'welcome.ans','wb').write(welcome_screen())
    for name,data in doors_screens().items():
        open(OUT+name,'wb').write(data)
    for name,(en,de) in lists().items():
        open(OUT+name+'.ans','wb').write(en.encode('cp437'))
        if de is not None:
            open(OUT+name+'.de.ans','wb').write(de.encode('cp437'))
    print('ok')

if __name__=='__main__':
    main()
