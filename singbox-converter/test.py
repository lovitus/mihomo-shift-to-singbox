import json
with open('plain_noemoji.json','r',encoding='utf-8') as f:
 c=json.load(f)
groups=[g for g in c.get('outbounds',[]) if g.get('type') in ('urltest','selector','fallback')]
print('Groups inside my converter output:', len(groups))
kr = [g['tag'] for g in groups if 'KR' in g['tag']]
print('KR groups:', kr)
