// Development-only reference runner for the pinned upstream Recast source.
// Compile separately; ordinary Go builds and tests do not execute this program.
#include "Recast.h"
#include "DetourNavMesh.h"
#include "DetourNavMeshBuilder.h"
#include "DetourNavMeshQuery.h"
#include <iostream>
#include <vector>
#include <iomanip>
#include <cstring>
#include <algorithm>
#include <cstdlib>

template<class T> void array(const T* values, int count);

struct Probe { float start[3], end[3], extents[3]; };

void runtimeProbes(rcConfig& c, rcPolyMesh& mesh, rcPolyMeshDetail& detail, const std::vector<Probe>& probes) {
    dtNavMesh nav; dtNavMeshQuery query; dtQueryFilter filter;
    bool ready=false;
    if(mesh.npolys) {
        for(int i=0;i<mesh.npolys;++i)mesh.flags[i]=1;
        dtNavMeshCreateParams p;std::memset(&p,0,sizeof(p));
        p.verts=mesh.verts;p.vertCount=mesh.nverts;p.polys=mesh.polys;p.polyAreas=mesh.areas;p.polyFlags=mesh.flags;p.polyCount=mesh.npolys;p.nvp=mesh.nvp;
        p.detailMeshes=detail.meshes;p.detailVerts=detail.verts;p.detailVertsCount=detail.nverts;p.detailTris=detail.tris;p.detailTriCount=detail.ntris;
        std::memcpy(p.bmin,mesh.bmin,sizeof(p.bmin));std::memcpy(p.bmax,mesh.bmax,sizeof(p.bmax));
        p.cs=c.cs;p.ch=c.ch;p.walkableHeight=c.walkableHeight*c.ch;p.walkableRadius=c.walkableRadius*c.cs;p.walkableClimb=c.walkableClimb*c.ch;p.buildBvTree=true;
        unsigned char* data=0;int size=0;
        if(!dtCreateNavMeshData(&p,&data,&size))std::exit(17);
        if(dtStatusFailed(nav.init(data,size,DT_TILE_FREE_DATA)))std::exit(18);
        if(dtStatusFailed(query.init(&nav,2048)))std::exit(19);
        ready=true;
    }
    std::cout << ",\"probes\":[" << std::boolalpha;
    for(size_t i=0;i<probes.size();++i) {
        if(i)std::cout << ',';
        const Probe& p=probes[i];dtPolyRef a=0,b=0;float ap[3]={0,0,0},bp[3]={0,0,0};
        unsigned int pathStatus=0,slicedStatus=0,rayStatus=0,heightStatus=0;bool complete=false,partial=false,hit=false;float height=0,fraction=0;
        if(ready) {
            query.findNearestPoly(p.start,p.extents,&filter,&a,ap);query.findNearestPoly(p.end,p.extents,&filter,&b,bp);
            if(a) {
                heightStatus=query.getPolyHeight(a,ap,&height);
                float t=0,normal[3];dtPolyRef visited[64];int count=0;
                rayStatus=query.raycast(a,ap,p.end,&filter,&t,normal,visited,&count,64);
                hit=t<1;fraction=std::min(t,1.0f);
            }
            if(a&&b) {
                dtPolyRef path[64];int count=0;pathStatus=query.findPath(a,b,ap,bp,&filter,path,&count,64);
                complete=count>0&&path[count-1]==b&&!dtStatusDetail(pathStatus,DT_PARTIAL_RESULT|DT_BUFFER_TOO_SMALL|DT_OUT_OF_NODES);
                partial=dtStatusDetail(pathStatus,DT_PARTIAL_RESULT);
                slicedStatus=query.initSlicedFindPath(a,b,ap,bp,&filter,0);
                while(dtStatusInProgress(slicedStatus))slicedStatus=query.updateSlicedFindPath(8,0);
                if(!dtStatusFailed(slicedStatus))slicedStatus=query.finalizeSlicedFindPath(path,&count,64);
            }
        }
        std::cout << "{\"startFound\":" << (a!=0) << ",\"endFound\":" << (b!=0) << ",\"start\":";array(ap,3);
        std::cout << ",\"end\":";array(bp,3);
        std::cout << ",\"pathStatus\":" << pathStatus << ",\"slicedStatus\":" << slicedStatus << ",\"rayStatus\":" << rayStatus << ",\"heightStatus\":" << heightStatus;
        std::cout << ",\"complete\":" << complete << ",\"partial\":" << partial << ",\"height\":" << height << ",\"hit\":" << hit << ",\"fraction\":" << fraction << '}';
    }
    std::cout << ']';
}

template<class T> void array(const T* values, int count) {
    std::cout << '[';
    for (int i=0;i<count;++i) { if(i) std::cout << ','; std::cout << +values[i]; }
    std::cout << ']';
}

int main() {
    int version=0, mode=0, wantLayers=0, nv=0, nt=0, nvol=0;
    rcConfig c; std::memset(&c,0,sizeof(c));
    std::cin >> version >> mode >> wantLayers;
    if(version!=1) return 2;
    std::cin >> c.width >> c.height >> c.borderSize >> c.cs >> c.ch;
    for(int i=0;i<3;++i) std::cin >> c.bmin[i];
    for(int i=0;i<3;++i) std::cin >> c.bmax[i];
    std::cin >> c.walkableSlopeAngle >> c.walkableHeight >> c.walkableClimb >> c.walkableRadius;
    std::cin >> c.maxEdgeLen >> c.maxSimplificationError >> c.minRegionArea >> c.mergeRegionArea;
    std::cin >> c.maxVertsPerPoly >> c.detailSampleDist >> c.detailSampleMaxError;
    std::cin >> nv >> nt;
    std::vector<float> verts(nv*3);std::vector<int> tris(nt*3);std::vector<unsigned char> areas(nt);
    for(int i=0;i<nv*3;++i) std::cin >> verts[i];
    for(int i=0;i<nt*3;++i) std::cin >> tris[i];
    for(int i=0;i<nt;++i) {int a;std::cin >> a;areas[i]=(unsigned char)a;}
    if(!std::cin || nv<3 || nt<1) return 3;
    rcContext ctx;
    rcHeightfield hf;
    if(!rcCreateHeightfield(&ctx,hf,c.width,c.height,c.bmin,c.bmax,c.cs,c.ch)) return 4;
    rcClearUnwalkableTriangles(&ctx,c.walkableSlopeAngle,&verts[0],nv,&tris[0],nt,&areas[0]);
    if(!rcRasterizeTriangles(&ctx,&verts[0],nv,&tris[0],&areas[0],nt,hf,c.walkableClimb)) return 5;
    rcFilterLowHangingWalkableObstacles(&ctx,c.walkableClimb,hf);
    rcFilterLedgeSpans(&ctx,c.walkableHeight,c.walkableClimb,hf);
    rcFilterWalkableLowHeightSpans(&ctx,c.walkableHeight,hf);
    rcCompactHeightfield chf;
    if(!rcBuildCompactHeightfield(&ctx,c.walkableHeight,c.walkableClimb,hf,chf)) return 6;
    std::cin >> nvol;
    for(int i=0;i<nvol;++i) {
        int shape,area,n;float lo[3],hi[3],radius,height;
        std::cin >> shape >> area;
        for(int k=0;k<3;++k) std::cin >> lo[k];
        for(int k=0;k<3;++k) std::cin >> hi[k];
        std::cin >> radius >> height >> n;
        std::vector<float> points(n*3);for(int k=0;k<n*3;++k) std::cin >> points[k];
        if(shape==0) rcMarkBoxArea(&ctx,lo,hi,(unsigned char)area,chf);
        else if(shape==1) rcMarkConvexPolyArea(&ctx,&points[0],n,lo[1],hi[1],(unsigned char)area,chf);
        else if(shape==2) rcMarkCylinderArea(&ctx,lo,radius,height,(unsigned char)area,chf);
        else return 7;
    }
    int nprobes=0;std::cin >> nprobes;std::vector<Probe> probes(nprobes);
    for(int i=0;i<nprobes;++i) {for(int j=0;j<3;++j)std::cin>>probes[i].start[j];for(int j=0;j<3;++j)std::cin>>probes[i].end[j];for(int j=0;j<3;++j)std::cin>>probes[i].extents[j];}
    if(!std::cin || !rcErodeWalkableArea(&ctx,c.walkableRadius,chf)) return 8;
    rcHeightfieldLayerSet layers;
    if(wantLayers&&!rcBuildHeightfieldLayers(&ctx,chf,c.borderSize,c.walkableHeight,layers)) return 9;
    if(mode==0) {if(!rcBuildRegionsMonotone(&ctx,chf,c.borderSize,c.minRegionArea,c.mergeRegionArea))return 10;}
    else if(mode==1) {if(!rcBuildDistanceField(&ctx,chf)||!rcBuildRegions(&ctx,chf,c.borderSize,c.minRegionArea,c.mergeRegionArea))return 11;}
    else if(mode==2) {if(!rcBuildLayerRegions(&ctx,chf,c.borderSize,c.minRegionArea))return 12;}
    else return 13;
    rcContourSet contours;rcPolyMesh mesh;rcPolyMeshDetail detail;
    if(!rcBuildContours(&ctx,chf,c.maxSimplificationError,c.maxEdgeLen,contours,RC_CONTOUR_TESS_WALL_EDGES))return 14;
    if(!rcBuildPolyMesh(&ctx,contours,c.maxVertsPerPoly,mesh))return 15;
    if(!rcBuildPolyMeshDetail(&ctx,mesh,chf,c.detailSampleDist,c.detailSampleMaxError,detail))return 16;
    std::cout << std::setprecision(9);
    std::cout << "{\"reference\":\"9f4ce64458dfae86e1239c525ddc219c4e9e06f1\",\"layers\":" << layers.nlayers << ",\"maxDistance\":" << chf.maxDistance << ",\"meshes\":";
    array(detail.meshes,detail.nmeshes*4);std::cout << ",\"verts\":";array(detail.verts,detail.nverts*3);
    std::cout << ",\"tris\":";array(detail.tris,detail.ntris*4);std::cout << ",\"areas\":";array(mesh.areas,mesh.npolys);
    runtimeProbes(c,mesh,detail,probes);
    std::cout << "}\n";
}
