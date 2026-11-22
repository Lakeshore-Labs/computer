import * as THREE from 'three';
import {STLLoader} from 'three/addons/loaders/STLLoader.js';
import {OrbitControls} from 'three/addons/controls/OrbitControls.js';

const payload=JSON.parse(document.getElementById('models').textContent);
const host=document.getElementById('stage');
const scene=new THREE.Scene();
scene.background=new THREE.Color('#eae8de');
const renderer=new THREE.WebGLRenderer({antialias:true,preserveDrawingBuffer:true});
renderer.setPixelRatio(Math.min(devicePixelRatio,2));
renderer.shadowMap.enabled=true;renderer.shadowMap.type=THREE.PCFSoftShadowMap;
host.append(renderer.domElement);
const camera=new THREE.OrthographicCamera(-85,85,70,-70,0.1,1500);
camera.up.set(0,0,1);
const controls=new OrbitControls(camera,renderer.domElement);
controls.enableDamping=false;controls.maxPolarAngle=Math.PI*.47;controls.minZoom=.55;controls.maxZoom=3.4;
const ambient=new THREE.HemisphereLight(0xfbf9ef,0x76725a,2.25);scene.add(ambient);
const sun=new THREE.DirectionalLight(0xffefcb,2.85);sun.position.set(-30,-80,160);sun.castShadow=true;
sun.shadow.mapSize.set(1024,1024);sun.shadow.camera.left=-190;sun.shadow.camera.right=190;sun.shadow.camera.top=190;sun.shadow.camera.bottom=-190;sun.shadow.camera.far=500;
sun.target.position.set(55,48,0);scene.add(sun,sun.target);
const plane=new THREE.Mesh(new THREE.PlaneGeometry(1000,1000),new THREE.MeshStandardMaterial({color:0xe9e6db,roughness:1}));
plane.position.z=-.18;plane.receiveShadow=true;scene.add(plane);
const mat=new THREE.MeshStandardMaterial({vertexColors:true,roughness:.95});
const unpack=s=>Uint8Array.from(atob(s),c=>c.charCodeAt(0));
const palette=[new THREE.Color('#86a8a3'),new THREE.Color('#b0b99a'),new THREE.Color('#738c71'),new THREE.Color('#d4c490')];
const loader=new STLLoader();
let mesh,selected;
function active(key){
 selected=payload.find(m=>m.factor===key);
 const bytes=unpack(selected.base64);
 const geometry=loader.parse(bytes.buffer);
 const pos=geometry.getAttribute('position'),colors=[];
 for(let i=0;i<pos.count;i++){
  const z=pos.getZ(i),height=Math.max(0,(z-2.5)/key);
  const color=z<2.49?new THREE.Color('#a99b77'):height<.01?palette[0]:height<.7?palette[1].clone().lerp(palette[2],height/.7):palette[2].clone().lerp(palette[3],Math.min(1,(height-.7)/1.6));
  colors.push(color.r,color.g,color.b);
 }
 geometry.setAttribute('color',new THREE.Float32BufferAttribute(colors,3));
 if(mesh){scene.remove(mesh);mesh.geometry.dispose()}
 mesh=new THREE.Mesh(geometry,mat);mesh.castShadow=true;mesh.receiveShadow=true;scene.add(mesh);
 document.getElementById('metrics').textContent=selected.width.toFixed(0)+' × '+selected.depth.toFixed(1)+' × '+selected.height.toFixed(1)+' mm';
 document.getElementById('faces').textContent=Intl.NumberFormat('en').format(pos.count/3)+' triangles';
 const download=document.getElementById('download');
 if(download.href.startsWith('blob:'))URL.revokeObjectURL(download.href);
 download.href=URL.createObjectURL(new Blob([bytes],{type:'model/stl'}));download.download=selected.file;
 host.dataset.triangles=String(pos.count/3);host.dataset.factor=String(key);
 frame();
}
function frame(){renderer.render(scene,camera)}
function reset(){camera.position.set(160,-94,105);camera.zoom=1;controls.target.set(55,selected?selected.depth/2:50,2.5);camera.updateProjectionMatrix();controls.update();frame()}
function resize(){const w=host.clientWidth,h=host.clientHeight,r=w/h,half=Math.max(70,84/r);camera.left=-half*r;camera.right=half*r;camera.top=half;camera.bottom=-half;camera.updateProjectionMatrix();renderer.setSize(w,h);frame()}
controls.addEventListener('change',frame);
document.getElementById('scale').addEventListener('change',e=>active(Number(e.target.value)));
document.getElementById('reset').addEventListener('click',reset);
new ResizeObserver(resize).observe(host);
resize();active(4);reset();host.dataset.ready='true';
