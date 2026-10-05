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
            out.append(line)
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
    ('R','menu.item.newscan','screen.desc.newscan',False),None,
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
    both('msgareas-columns',lambda de: sgr(7)+'    '+T('common.area',-53,de)+' '+T('col.total',6,de)+' '+T('common.new_2',6,de)+' '+T('col.yours',7,de)+ESC+'0m\r\n'+rule())
    out['msgareas-row']=(sgr(14)+'{NEWFLAG:-3} '+sgr(15)+'{AREANAME:-53} '+sgr(8)+'{TOTAL:6} '+sgr(14)+'{NEW:6} '+sgr(7)+'{YOURS:7}'+ESC+'0m',None)
    out['msgareas-row-selected']=(SELECTED+'{NEWFLAG:-3} {AREANAME:-53} {TOTAL:6} {NEW:6} {YOURS:7}'+ESC+'0m',None)
    out['msgareas-network']=(divider('NETWORK'),None)

    out['msglist']=(list_head(msghead(),'{AREANAME}'),None)
    both('msglist-columns',lambda de: sgr(7)+'    '+T('common.subject',-39,de)+' '+T('common.from',-18,de)+' '+T('col.date',16,de)+ESC+'0m\r\n'+rule())
    out['msglist-row']=(sgr(14)+'{NEWFLAG:-3} '+sgr(15)+'{SUBJECT:-39} '+sgr(7)+'{FROM:-18} '+sgr(8)+'{DATE:16}'+ESC+'0m',None)
    out['msglist-row-selected']=(SELECTED+'{NEWFLAG:-3} {SUBJECT:-39} {FROM:-18} {DATE:16}'+ESC+'0m',None)

    both('msgread',lambda de: list_head(msghead(),sgr(14)+'{AREANAME}'+sgr(8)+' \u00b7 '+sgr(7)+T('screen.msg_pos',None,de)))
    both('msgread-meta',lambda de: sgr(6)+T('msg.from',-9,de)+sgr(15)+'{FROM:-40}'+sgr(6)+' '+T('msg.date',None,de)+' '+sgr(7)+'{DATE}\r\n'
        +sgr(6)+T('msg.to',-9,de)+sgr(15)+'{TO:-40}\r\n'+sgr(6)+T('msg.subject',-9,de)+sgr(14)+'{SUBJECT}\r\n'+rule())
    out['msgread-footer']=(sgr(8)+'{SCROLLSTATUS}'+ESC+'0m\r\n'+sgr(7)+'{HINT}'+ESC+'0m',None)
    both('msgpost',lambda de: list_head(msghead(),sgr(14)+T('screen.msgpost',None,de)+sgr(8)+' \u00b7 '+sgr(7)+'{AREANAME}'))

    # Netmail is private mail, not echomail: violet, as it always was.
    def comet(a): a.put(58,1,[('\u00b7',8),('\u00b7',8),('-',8),('-',5),('\u2500',13),('\u2500',15),('*',15)])
    nethead=lambda: strip(VIOLET,7,33,motif=comet)
    both('netmail',lambda de: list_head(nethead(),T('common.netmail',None,de)))
    both('netmail-columns',lambda de: sgr(7)+'    '+T('common.subject',-39,de)+' '+T('common.from',-18,de)+' '+T('col.date',16,de)+ESC+'0m\r\n'+rule())
    out['netmail-row']=(sgr(13)+'{NEWFLAG:-3} '+sgr(15)+'{SUBJECT:-39} '+sgr(7)+'{FROM:-18} '+sgr(8)+'{DATE:16}'+ESC+'0m',None)
    out['netmail-row-selected']=(ESC+'1;37;45m{NEWFLAG:-3} {SUBJECT:-39} {FROM:-18} {DATE:16}'+ESC+'0m',None)
    both('netread',lambda de: list_head(nethead(),sgr(14)+T('screen.netread',None,de)+sgr(8)+' \u00b7 '+sgr(7)+T('screen.msg_pos',None,de)))
    both('netread-meta',lambda de: sgr(5)+T('msg.from',-9,de)+sgr(15)+'{FROM:-40}'+sgr(5)+' '+T('msg.date',None,de)+' '+sgr(7)+'{DATE}\r\n'
        +sgr(5)+T('msg.to',-9,de)+sgr(15)+'{TO:-40}\r\n'+sgr(5)+T('msg.subject',-9,de)+sgr(13)+'{SUBJECT}\r\n'+rule())
    out['netread-footer']=out['msgread-footer']

    # Files: green, with the files menu's asteroids.
    def rocks(a):
        a.disc(66.0,3.0,2.7,(7,8,8),light=(-0.7,-0.7)); a.disc(72.5,1.5,1.0,(7,8,8))
        for (x,y,ch,fg) in [(60,0,'\u25a0',8),(71,1,'\u25a0',7),(74,2,'\u2219',8),(62,2,'\u2219',7)]: a.cells[y][x]=(ch,fg,0)
    filhead=lambda: strip(GREEN,23,9,motif=rocks)
    GSEL=ESC+'1;37;42m'
    both('filareas',lambda de: list_head(filhead(),T('common.file_areas',None,de)))
    both('filareas-columns',lambda de: sgr(7)+'    '+T('common.area',-53,de)+' '+T('col.total',6,de)+' '+T('common.new_2',6,de)+' '+T('col.yours',7,de)+ESC+'0m\r\n'+rule())
    out['filareas-row']=(sgr(10)+'{NEWFLAG:-3} '+sgr(15)+'{AREANAME:-53} '+sgr(8)+'{TOTAL:6} '+sgr(10)+'{NEW:6} '+sgr(7)+'{YOURS:7}'+ESC+'0m',None)
    out['filareas-row-selected']=(GSEL+'{NEWFLAG:-3} {AREANAME:-53} {TOTAL:6} {NEW:6} {YOURS:7}'+ESC+'0m',None)
    out['filareas-network']=(divider('NETWORK',2),None)
    out['fillist']=(list_head(filhead(),'{AREANAME}'),None)
    both('fillist-columns',lambda de: sgr(7)+'    '+T('common.filename',-30,de)+' '+T('col.by',-16,de)+' '+T('common.size',10,de)+' '+T('col.date',16,de)+ESC+'0m\r\n'+rule())
    out['fillist-row']=(sgr(10)+'{NEWFLAG:-3} '+sgr(15)+'{FILENAME:-30} '+sgr(7)+'{BY:-16} '+sgr(2)+'{SIZE:10} '+sgr(8)+'{DATE:16}'+ESC+'0m',None)
    out['fillist-row-selected']=(GSEL+'{NEWFLAG:-3} {FILENAME:-30} {BY:-16} {SIZE:10} {DATE:16}'+ESC+'0m',None)
    both('filread',lambda de: list_head(filhead(),sgr(14)+'{AREANAME}'+sgr(8)+' \u00b7 '+sgr(7)+T('screen.filread_pos',None,de)))
    both('filread-meta',lambda de: sgr(2)+T('files.filename',-11,de)+sgr(15)+'{FILENAME:-40}'+sgr(2)+' '+T('files.size',None,de)+' '+sgr(7)+'{SIZE}\r\n'
        +sgr(2)+T('files.uploaded',-11,de)+sgr(7)+'{DATE}'+sgr(2)+' '+T('files.by',None,de)+' '+sgr(15)+'{BY}\r\n'
        +sgr(2)+T('files.downloads',-11,de)+sgr(7)+'{DOWNLOADS}\r\n'+rule())
    out['filread-footer']=out['msgread-footer']
    return out

def main():
    for name in MENUS:
        for de in (False,True):
            open(OUT+name+('.de' if de else '')+'.ans','wb').write(menu_screen(name,de))
    for de in (False,True):
        open(OUT+'logoff'+('.de' if de else '')+'.ans','wb').write(logoff_screen(de))
    for name,(en,de) in lists().items():
        open(OUT+name+'.ans','wb').write(en.encode('cp437'))
        if de is not None:
            open(OUT+name+'.de.ans','wb').write(de.encode('cp437'))
    print('ok')

if __name__=='__main__':
    main()
